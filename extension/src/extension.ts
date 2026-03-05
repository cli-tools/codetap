import * as vscode from 'vscode';
import { SessionProvider } from './sessionProvider';
import { SessionWatcher } from './sessionWatcher';
import { CodetapResolverProvider } from './resolver';
import { Session, SessionLocation } from './types';

export function activate(context: vscode.ExtensionContext) {
	const config = vscode.workspace.getConfiguration('codetap');
	const pollInterval = config.get<number>('pollInterval', 3000);

	// Detect whether we're running on the UI (local) side or remote (workspace) side.
	// vscode.env.remoteName is set when connected to a remote.
	// context.extension.extensionKind tells us where THIS instance is running.
	const isRemote = context.extension.extensionKind === vscode.ExtensionKind.Workspace
		&& vscode.env.remoteName !== undefined;
	const location: SessionLocation = isRemote ? 'remote' : 'local';
	const socketDir = isRemote
		? config.get<string>('remoteSocketDir', '/dev/shm/codetap')
		: config.get<string>('socketDir', '/dev/shm/codetap');

	// Register the remote authority resolver for codetap:// URIs (only on UI side).
	// On the remote/workspace side, connecting to sessions is not supported — the
	// resolver must run on the UI side where it can open new windows.
	let resolverAvailable = false;
	if (!isRemote) {
		resolverAvailable = CodetapResolverProvider.register(context);
		if (!resolverAvailable) {
			void vscode.window.showWarningMessage(
				'CodeTap remote connections require resolver API access. Launch VS Code with: --enable-proposed-api codetap.codetap'
			);
		}
	}

	// Set up session watcher and tree view
	const watcher = new SessionWatcher(socketDir, pollInterval, location);
	const provider = new SessionProvider(watcher);

	context.subscriptions.push(
		vscode.window.registerTreeDataProvider('codetap.sessions', provider),

		vscode.commands.registerCommand('codetap.refresh', () => {
			provider.refresh();
		}),

		vscode.commands.registerCommand('codetap.connect', async (session?: Session) => {
			if (!resolverAvailable) {
				vscode.window.showErrorMessage(
					'CodeTap connect requires remote authority resolver support in VS Code.'
				);
				return;
			}

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

			const socketPathEncoded = encodeURIComponent(session.socketPath);
			const folder = session.metadata.folder || '/';
			const uri = vscode.Uri.parse(
				`vscode-remote://codetap+${socketPathEncoded}${folder}`
			);

			await vscode.commands.executeCommand('vscode.openFolder', uri, {
				forceNewWindow: false
			});
		}),
	);

	watcher.start();
	context.subscriptions.push({ dispose: () => watcher.stop() });
}

export function deactivate() {}
