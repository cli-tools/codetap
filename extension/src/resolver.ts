import * as vscode from 'vscode';
import * as fs from 'fs';
import * as net from 'net';

type ManagedMessagePassing = {
	onDidReceiveMessage: vscode.Event<Uint8Array>;
	onDidClose: vscode.Event<void>;
	send(data: Uint8Array): void;
	end(): void;
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

type RemoteResolverAPI = {
	registerRemoteAuthorityResolver?: (
		authorityPrefix: string,
		resolver: { resolve(authority: string): Thenable<ManagedResolvedAuthority> }
	) => vscode.Disposable;
};

type VscodeResolverAPI = {
	ManagedResolvedAuthority?: ManagedResolvedAuthorityCtor;
	RemoteAuthorityResolverError?: ResolverErrorFactory;
};

export class CodetapResolverProvider {
	static register(context: vscode.ExtensionContext): boolean {
		const workspaceAPI = vscode.workspace as unknown as RemoteResolverAPI;
		const resolverAPI = vscode as unknown as VscodeResolverAPI;

		const registerRemoteAuthorityResolver = workspaceAPI.registerRemoteAuthorityResolver;
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

					// Read the connection token from the paired .token file
					const tokenPath = socketPath.replace(/\.sock$/, '.token');
					let connectionToken: string | undefined;
					try {
						connectionToken = fs.readFileSync(tokenPath, 'utf-8').trim();
					} catch {
						// Token might not exist; server may use --without-connection-token
					}

					// Create a managed authority using a socket connection factory
					const makeConnection = (): Thenable<ManagedMessagePassing> => {
						return new Promise((resolve, reject) => {
							const socket = net.createConnection(socketPath, () => {
								const reader = new vscode.EventEmitter<Uint8Array>();
								const writer = new vscode.EventEmitter<void>();

								socket.on('data', (data: Buffer) => {
									reader.fire(new Uint8Array(data));
								});

								socket.on('close', () => {
									writer.fire();
								});

								socket.on('error', () => {
									writer.fire();
								});

								resolve({
									onDidReceiveMessage: reader.event,
									onDidClose: writer.event,
									send(data: Uint8Array): void {
										socket.write(data);
									},
									end(): void {
										socket.end();
									}
								});
							});
							socket.on('error', reject);
						});
					};

					const resolved = new managedResolvedAuthority(makeConnection);
					if (connectionToken) {
						resolved.connectionToken = connectionToken;
					}
					return Promise.resolve(resolved);
				}
			});

			context.subscriptions.push(resolver);
			return true;
		} catch {
			// Stable builds can reject this API (proposal: resolvers).
			return false;
		}
	}
}
