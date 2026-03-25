import * as vscode from 'vscode';
import * as net from 'net';
import * as crypto from 'crypto';
import { SessionProvider } from './sessionProvider';
import { SessionWatcher } from './sessionWatcher';
import { CodetapResolver } from './resolver';
import { Session, SessionLocation } from './types';

const AUTHORITY = 'codetap';
const CLIENT_ID = `vscode-${crypto.randomBytes(4).toString('hex')}`;

// Keep lease connections alive for the lifetime of the extension.
const leases: net.Socket[] = [];

export function activate(context: vscode.ExtensionContext) {
	const config = vscode.workspace.getConfiguration('codetap');
	const pollInterval = config.get<number>('pollInterval', 3000);

	const isRemote = context.extension.extensionKind === vscode.ExtensionKind.Workspace
		&& vscode.env.remoteName !== undefined;
	const location: SessionLocation = isRemote ? 'remote' : 'local';
	const socketDir = isRemote
		? config.get<string>('remoteSocketDir', '/dev/shm/codetap')
		: config.get<string>('socketDir', '/dev/shm/codetap');

	const watcher = new SessionWatcher(socketDir, pollInterval, location);
	const provider = new SessionProvider(watcher);
	const resolver = new CodetapResolver();

	context.subscriptions.push(
		vscode.workspace.registerRemoteAuthorityResolver(AUTHORITY, resolver),
		vscode.window.registerTreeDataProvider('codetap.sessions', provider),

		vscode.commands.registerCommand('codetap.refresh', () => {
			provider.refresh();
		}),

		vscode.commands.registerCommand('codetap.connect', async (session?: Session) => {
			if (!session) {
				const sessions = await watcher.getSessions();
				const alive = sessions.filter(s => s.alive);
				if (alive.length === 0) {
					vscode.window.showInformationMessage('No alive CodeTap sessions found.');
					return;
				}
				const picked = await vscode.window.showQuickPick(
					alive.map(s => ({
						label: s.name,
						description: s.metadata.folder,
						session: s
					})),
					{ placeHolder: 'Select a CodeTap session' }
				);
				if (!picked) {
					return;
				}
				session = picked.session;
			}

			const folder = session.metadata.folder;
			if (!folder) {
				vscode.window.showErrorMessage('Session has no folder path.');
				return;
			}

			try {
				const { token, conn } = await ctapConnect(
					session.ctlSocketPath,
					session.metadata.commit,
				);
				leases.push(conn);
				resolver.setSession(session.name, session.socketPath, token);
			} catch (err: unknown) {
				const msg = err instanceof Error ? err.message : String(err);
				vscode.window.showErrorMessage(`CodeTap connect failed: ${msg}`);
				return;
			}

			const uri = vscode.Uri.from({
				scheme: 'vscode-remote',
				authority: `${AUTHORITY}+${session.name}`,
				path: folder,
			});
			await vscode.commands.executeCommand('vscode.openFolder', uri, {
				forceNewWindow: false,
			});
		}),

		{ dispose: () => { for (const c of leases) c.destroy(); leases.length = 0; } },
	);

	watcher.start();
	context.subscriptions.push({ dispose: () => watcher.stop() });
}

export function deactivate() {
	for (const c of leases) c.destroy();
	leases.length = 0;
}

/**
 * Send CTAP1 CONNECT to the control socket. Returns the token and keeps
 * the connection alive as a lease.
 */
function ctapConnect(
	ctlSocketPath: string,
	commit: string,
): Promise<{ token: string; conn: net.Socket }> {
	return new Promise((resolve, reject) => {
		const conn = net.createConnection(ctlSocketPath, () => {
			conn.write(`CTAP1 CONNECT ${commit} ${CLIENT_ID}\n`);
		});

		let data = '';
		conn.on('data', (chunk: Buffer) => {
			data += chunk.toString();
			if (data.includes('\n')) {
				const line = data.trim();
				if (line.startsWith('OK')) {
					const token = line.slice(3).trim();
					conn.removeAllListeners('data');
					conn.removeAllListeners('timeout');
					resolve({ token, conn });
				} else {
					conn.destroy();
					reject(new Error(line));
				}
			}
		});

		conn.on('error', reject);
		conn.setTimeout(5000, () => {
			conn.destroy();
			reject(new Error('timeout connecting to codetap session'));
		});
	});
}
