import * as vscode from 'vscode';
import { SessionWatcher } from './sessionWatcher';
import { Session } from './types';

export class SessionProvider implements vscode.TreeDataProvider<Session> {
	private readonly _onDidChangeTreeData = new vscode.EventEmitter<void>();
	readonly onDidChangeTreeData = this._onDidChangeTreeData.event;

	constructor(private watcher: SessionWatcher) {
		watcher.onDidChange(() => this._onDidChangeTreeData.fire());
	}

	refresh(): void {
		this._onDidChangeTreeData.fire();
	}

	getTreeItem(session: Session): vscode.TreeItem {
		const item = new vscode.TreeItem(
			session.name,
			vscode.TreeItemCollapsibleState.None
		);
		item.description = session.metadata.folder;
		item.tooltip = [
			`Name: ${session.name}`,
			`Commit: ${session.metadata.commit}`,
			`Arch: ${session.metadata.arch}`,
			`PID: ${session.metadata.pid}`,
			`Folder: ${session.metadata.folder}`,
			`Status: ${session.alive ? 'alive' : 'dead'}`,
		].join('\n');
		item.iconPath = new vscode.ThemeIcon(
			session.alive ? 'circle-filled' : 'circle-outline'
		);
		item.contextValue = session.alive ? 'aliveSession' : 'deadSession';
		return item;
	}

	async getChildren(): Promise<Session[]> {
		return this.watcher.getSessions();
	}
}
