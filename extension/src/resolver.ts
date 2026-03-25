import * as vscode from 'vscode';
import * as net from 'net';

interface SessionInfo {
	socketPath: string;
	token: string;
}

/**
 * Resolves codetap+<name> remote authorities by connecting VS Code
 * directly to the VS Code Server Unix socket.
 */
export class CodetapResolver implements vscode.RemoteAuthorityResolver {
	private sessions = new Map<string, SessionInfo>();

	setSession(name: string, socketPath: string, token: string): void {
		this.sessions.set(name, { socketPath, token });
	}

	resolve(authority: string): vscode.ResolverResult {
		const name = authority.replace(/^codetap\+/, '');
		const session = this.sessions.get(name);
		if (!session) {
			throw vscode.RemoteAuthorityResolverError.NotAvailable(
				`No codetap session "${name}" — connect via the CodeTap panel first.`,
			);
		}

		const { socketPath, token } = session;

		return new vscode.ManagedResolvedAuthority(() => {
			return new Promise<vscode.ManagedMessagePassing>((resolve, reject) => {
				const socket = net.createConnection(socketPath, () => {
					const onDidReceiveMessage = new vscode.EventEmitter<Uint8Array>();
					const onDidClose = new vscode.EventEmitter<Error | undefined>();
					const onDidEnd = new vscode.EventEmitter<void>();

					socket.on('data', (chunk: Buffer) => onDidReceiveMessage.fire(chunk));
					socket.on('close', (hadError: boolean) =>
						onDidClose.fire(hadError ? new Error('socket closed with error') : undefined));
					socket.on('end', () => onDidEnd.fire());

					resolve({
						onDidReceiveMessage: onDidReceiveMessage.event,
						onDidClose: onDidClose.event,
						onDidEnd: onDidEnd.event,
						send: (data: Uint8Array) => { socket.write(data); },
						end: () => { socket.end(); },
					});
				});

				socket.on('error', reject);
			});
		}, token || undefined);
	}
}
