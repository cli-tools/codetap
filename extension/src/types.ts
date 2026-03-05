export interface SessionMetadata {
	name: string;
	commit: string;
	arch: string;
	folder: string;
	pid: number;
	started_at: string;
}

export type SessionLocation = 'local' | 'remote';

export interface Session {
	name: string;
	socketPath: string;
	ctlSocketPath: string;
	metadata: SessionMetadata;
	alive: boolean;
	location: SessionLocation;
}
