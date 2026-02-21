import * as vscode from 'vscode';
import * as fs from 'fs';
import * as net from 'net';

export class CodetapResolverProvider {
	static register(context: vscode.ExtensionContext): void {
		// Register the codetap remote authority resolver.
		// URI format: vscode-remote://codetap+<encodedSocketPath>/<folder>
		const resolver = vscode.workspace.registerRemoteAuthorityResolver('codetap', {
			resolve(authority: string): Thenable<vscode.ResolvedAuthority> {
				// authority = "codetap+<encodedSocketPath>"
				const parts = authority.split('+');
				if (parts.length < 2) {
					throw vscode.RemoteAuthorityResolverError.NotAvailable(
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
				const makeConnection = (): Thenable<vscode.ManagedMessagePassing> => {
					return new Promise((resolve, reject) => {
						const socket = net.createConnection(socketPath, () => {
							const reader = new vscode.EventEmitter<Buffer>();
							const writer = new vscode.EventEmitter<void>();

							socket.on('data', (data: Buffer) => {
								reader.fire(data);
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
								send(data: Buffer): void {
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

				const resolved = new vscode.ManagedResolvedAuthority(makeConnection);
				if (connectionToken) {
					resolved.connectionToken = connectionToken;
				}
				return Promise.resolve(resolved);
			}
		});

		context.subscriptions.push(resolver);
	}
}
