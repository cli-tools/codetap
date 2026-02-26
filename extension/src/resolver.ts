import * as vscode from 'vscode';
import * as fs from 'fs';
import * as net from 'net';
import * as path from 'path';

type ManagedMessagePassing = {
	onDidReceiveMessage: vscode.Event<Uint8Array>;
	onDidEnd: vscode.Event<void>;
	onDidClose: vscode.Event<Error | undefined>;
	send(data: Uint8Array): void;
	end(): void;
	drain?(): Thenable<void>;
};

type ManagedResolvedAuthority = {
	connectionToken?: string;
};

type ManagedResolvedAuthorityCtor = new (
	makeConnection: () => Thenable<ManagedMessagePassing>
) => ManagedResolvedAuthority;

type ResolverErrorFactory = {
	NotAvailable(message: string): Error;
};

type ResourceLabelFormatting = {
	label: string;
	separator: '/' | '\\' | '';
	tildify?: boolean;
	normalizeDriveLetter?: boolean;
	workspaceSuffix?: string;
	workspaceTooltip?: string;
	authorityPrefix?: string;
	stripPathStartingSeparator?: boolean;
};

type ResourceLabelFormatter = {
	scheme: string;
	authority?: string;
	formatting: ResourceLabelFormatting;
};

type RemoteResolverAPI = {
	registerRemoteAuthorityResolver?: (
		authorityPrefix: string,
		resolver: { resolve(authority: string): Thenable<ManagedResolvedAuthority> }
	) => vscode.Disposable;
	registerResourceLabelFormatter?: (
		formatter: ResourceLabelFormatter
	) => vscode.Disposable;
};

type VscodeResolverAPI = {
	ManagedResolvedAuthority?: ManagedResolvedAuthorityCtor;
	RemoteAuthorityResolverError?: ResolverErrorFactory;
};

function getVSCodeCommit(): string | undefined {
	// vscode.env.appCommit is available since VS Code 1.80+
	try {
		const commit = (vscode.env as any).appCommit;
		if (typeof commit === 'string' && /^[0-9a-f]{40}$/.test(commit)) {
			return commit;
		}
	} catch { /* fallback below */ }

	// Fallback: read product.json from the VS Code installation
	try {
		const productPath = path.join(vscode.env.appRoot, 'product.json');
		const product = JSON.parse(fs.readFileSync(productPath, 'utf-8'));
		if (typeof product.commit === 'string' && /^[0-9a-f]{40}$/.test(product.commit)) {
			return product.commit;
		}
	} catch { /* ignore */ }

	return undefined;
}

// Active control socket leases keyed by authority string.
const activeLeases = new Map<string, net.Socket>();

/**
 * Perform the CTAP1 CONNECT handshake on a control socket.
 * Returns the connection token and keeps the socket open as a lease.
 */
function ctapConnect(
	ctlSocketPath: string,
	commitHash: string,
	clientId: string,
	authority: string,
): Promise<{ token: string; ctlConn: net.Socket }> {
	return new Promise((resolve, reject) => {
		const conn = net.createConnection(ctlSocketPath, () => {
			conn.write(`CTAP1 CONNECT ${commitHash} ${clientId}\n`);
		});

		let data = '';
		conn.on('data', (chunk: Buffer) => {
			data += chunk.toString();
			if (data.includes('\n')) {
				const line = data.split('\n')[0].trim();
				if (line.startsWith('OK')) {
					const token = line.substring(3) || '';
					// Clear the CONNECT timeout so the lease stays open.
					conn.setTimeout(0);
					// Close any existing lease for this authority.
					const old = activeLeases.get(authority);
					if (old) {
						old.destroy();
					}
					activeLeases.set(authority, conn);
					resolve({ token, ctlConn: conn });
				} else if (line.startsWith('ERR ')) {
					conn.destroy();
					reject(new Error(line.substring(4)));
				} else {
					conn.destroy();
					reject(new Error('Unexpected response: ' + line));
				}
			}
		});

		conn.on('error', (err: Error) => {
			reject(err);
		});

		// Allow generous timeout for version switch (provisioning may take time).
		conn.setTimeout(120_000, () => {
			conn.destroy();
			reject(new Error('CONNECT timeout'));
		});
	});
}

export class CodetapResolverProvider {
	static register(context: vscode.ExtensionContext): boolean {
		const workspaceAPI = vscode.workspace as unknown as RemoteResolverAPI;
		const resolverAPI = vscode as unknown as VscodeResolverAPI;

		const registerRemoteAuthorityResolver = workspaceAPI.registerRemoteAuthorityResolver;
		const registerResourceLabelFormatter = workspaceAPI.registerResourceLabelFormatter;
		const managedResolvedAuthority = resolverAPI.ManagedResolvedAuthority;
		const remoteAuthorityResolverError = resolverAPI.RemoteAuthorityResolverError;
		if (
			!registerRemoteAuthorityResolver ||
			!managedResolvedAuthority ||
			!remoteAuthorityResolverError
		) {
			return false;
		}

		try {
			const dynamicHostFormatters = new Map<string, vscode.Disposable>();

			const registerHostLabelFormatter = (
				authority: string,
				socketPath: string
			): void => {
				if (!registerResourceLabelFormatter) {
					return;
				}
				if (dynamicHostFormatters.has(authority)) {
					return;
				}

				const socketName = path.basename(socketPath, '.sock');
				const formatter = registerResourceLabelFormatter({
					scheme: 'vscode-remote',
					authority,
					formatting: {
						label: '${path}',
						separator: '/',
						workspaceSuffix: `codetap(${socketName})`
					}
				});

				dynamicHostFormatters.set(authority, formatter);
				context.subscriptions.push({
					dispose: () => {
						dynamicHostFormatters.delete(authority);
						formatter.dispose();
					}
				});
			};

			// Register the codetap remote authority resolver.
			// URI format: vscode-remote://codetap+<encodedSocketPath>/<folder>
			const resolver = registerRemoteAuthorityResolver('codetap', {
				resolve(authority: string): Thenable<ManagedResolvedAuthority> {
					// authority = "codetap+<encodedSocketPath>"
					const parts = authority.split('+');
					if (parts.length < 2) {
						throw remoteAuthorityResolverError.NotAvailable(
							'Invalid codetap authority: ' + authority
						);
					}
					const socketPath = decodeURIComponent(parts.slice(1).join('+'));
					registerHostLabelFormatter(authority, socketPath);

					// Derive the control socket path from the data socket path.
					const ctlSocketPath = socketPath.replace(/\.sock$/, '.ctl.sock');

					const commitHash = getVSCodeCommit();
					if (!commitHash) {
						throw remoteAuthorityResolverError.NotAvailable(
							'Cannot determine VS Code commit hash'
						);
					}

					const clientId = String(process.pid);

					// Perform CONNECT handshake to get the connection token.
					return ctapConnect(ctlSocketPath, commitHash, clientId, authority)
						.then(({ token }) => {
							// Create a managed authority using a socket connection factory.
							const makeConnection = (): Thenable<ManagedMessagePassing> => {
								return new Promise((connResolve, connReject) => {
									const socket = net.createConnection(socketPath, () => {
										const reader = new vscode.EventEmitter<Uint8Array>();
										const ended = new vscode.EventEmitter<void>();
										const closed = new vscode.EventEmitter<Error | undefined>();
										let done = false;
										const finish = (err?: Error): void => {
											if (done) {
												return;
											}
											done = true;
											ended.fire();
											closed.fire(err);
											reader.dispose();
											ended.dispose();
											closed.dispose();
										};

										socket.on('data', (buf: Buffer) => {
											reader.fire(new Uint8Array(buf));
										});

										socket.on('end', () => {
											finish();
										});

										socket.on('close', (hadError: boolean) => {
											if (!hadError) {
												finish();
											}
										});

										socket.on('error', (err: Error) => {
											finish(err);
										});

										connResolve({
											onDidReceiveMessage: reader.event,
											onDidEnd: ended.event,
											onDidClose: closed.event,
											send(data: Uint8Array): void {
												socket.write(data);
											},
											end(): void {
												socket.end();
											},
											drain(): Thenable<void> {
												return new Promise<void>(drainResolve => {
													if (socket.writableNeedDrain) {
														socket.once('drain', () => drainResolve());
														return;
													}
													drainResolve();
												});
											}
										});
									});
									socket.on('error', connReject);
								});
							};

							const resolved = new managedResolvedAuthority(makeConnection);
							if (token) {
								resolved.connectionToken = token;
							}
							return resolved;
						})
						.catch((err: Error) => {
							throw remoteAuthorityResolverError.NotAvailable(
								'CTAP1 CONNECT failed: ' + err.message
							);
						});
				}
			});

			context.subscriptions.push(resolver);

			// Clean up all leases on extension deactivation.
			context.subscriptions.push({
				dispose: () => {
					for (const [, conn] of activeLeases) {
						conn.destroy();
					}
					activeLeases.clear();
				}
			});

			if (registerResourceLabelFormatter) {
				// Fallback label before a specific session authority is resolved.
				const formatter = registerResourceLabelFormatter({
					scheme: 'vscode-remote',
					authority: 'codetap+*',
					formatting: {
						label: '${path}',
						separator: '/',
						workspaceSuffix: 'codetap'
					}
				});
				context.subscriptions.push(formatter);
			}
			return true;
		} catch {
			// Stable builds can reject this API (proposal: resolvers).
			return false;
		}
	}
}
