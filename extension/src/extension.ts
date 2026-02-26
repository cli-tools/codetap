import * as vscode from 'vscode';
import { SessionProvider } from './sessionProvider';
import { SessionWatcher } from './sessionWatcher';
import { CodetapResolverProvider } from './resolver';
import { Session } from './types';

export function activate(context: vscode.ExtensionContext) {
	const config = vscode.workspace.getConfiguration('codetap');
	const socketDir = config.get<string>('socketDir', '/dev/shm/codetap');
	const pollInterval = config.get<number>('pollInterval', 3000);

	// Register the remote authority resolver for codetap:// URIs
	const resolverAvailable = CodetapResolverProvider.register(context);
	if (!resolverAvailable) {
		void vscode.window.showWarningMessage(
			'CodeTap remote connections require resolver API access. Launch VS Code with: --enable-proposed-api codetap.codetap'
		);
	}

	// Set up session watcher and tree view
	const watcher = new SessionWatcher(socketDir, pollInterval);
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
