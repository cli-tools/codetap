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
			files = fs.readdirSync(this.socketDir).filter(f => f.endsWith('.ctl.sock'));
		} catch {
			return sessions;
		}

		for (const file of files) {
			const name = file.replace(/\.ctl\.sock$/, '');
			const ctlSocketPath = path.join(this.socketDir, file);
			const socketPath = path.join(this.socketDir, name + '.sock');
			try {
				const metadata = await this.queryInfo(ctlSocketPath);
				sessions.push({ name, socketPath, ctlSocketPath, metadata, alive: true });
			} catch {
				// Control socket exists but not responding — dead session.
				sessions.push({
					name,
					socketPath,
					ctlSocketPath,
					metadata: { name, commit: '', arch: '', folder: '', pid: 0, started_at: '' },
					alive: false,
				});
			}
		}

		return sessions;
	}

	private queryInfo(ctlSocketPath: string): Promise<SessionMetadata> {
		return new Promise((resolve, reject) => {
			const conn = net.createConnection(ctlSocketPath, () => {
				conn.write('CTAP1 INFO\n');
			});

			let data = '';
			conn.on('data', (chunk: Buffer) => {
				data += chunk.toString();
				if (data.includes('\n')) {
					conn.destroy();
					try {
						resolve(JSON.parse(data.trim()));
					} catch (e) {
						reject(e);
					}
				}
			});

			conn.on('error', (err: Error) => reject(err));
			conn.setTimeout(2000, () => {
				conn.destroy();
				reject(new Error('timeout'));
			});
		});
	}
}
