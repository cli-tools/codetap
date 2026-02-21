import * as vscode from 'vscode';
import * as fs from 'fs';
import * as path from 'path';
import * as net from 'net';
import { Session, SessionMetadata } from './types';

export class SessionWatcher {
	private pollTimer: ReturnType<typeof setInterval> | undefined;
	private readonly _onDidChange = new vscode.EventEmitter<void>();
	readonly onDidChange = this._onDidChange.event;

	constructor(private socketDir: string, private pollInterval: number) {}

	start(): void {
		this.pollTimer = setInterval(() => this._onDidChange.fire(), this.pollInterval);
	}

	stop(): void {
		if (this.pollTimer) {
			clearInterval(this.pollTimer);
		}
		this._onDidChange.dispose();
	}

	async getSessions(): Promise<Session[]> {
		const sessions: Session[] = [];
		let files: string[];
		try {
			files = fs.readdirSync(this.socketDir).filter(f => f.endsWith('.json'));
		} catch {
			return sessions;
		}

		for (const file of files) {
			const name = path.basename(file, '.json');
			const jsonPath = path.join(this.socketDir, file);
			const socketPath = path.join(this.socketDir, name + '.sock');
			const tokenPath = path.join(this.socketDir, name + '.token');
			try {
				const raw = fs.readFileSync(jsonPath, 'utf-8');
				const metadata: SessionMetadata = JSON.parse(raw);
				const alive = await this.isAlive(socketPath);
				sessions.push({ name, socketPath, tokenPath, metadata, alive });
			} catch {
				// Skip corrupt or unreadable entries
			}
		}

		return sessions;
	}

	private isAlive(socketPath: string): Promise<boolean> {
		return new Promise(resolve => {
			const conn = net.createConnection(socketPath, () => {
				conn.destroy();
				resolve(true);
			});
			conn.on('error', () => resolve(false));
			conn.setTimeout(1000, () => {
				conn.destroy();
				resolve(false);
			});
		});
	}
}
