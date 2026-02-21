export interface SessionMetadata {
	name: string;
	commit: string;
	arch: string;
	folder: string;
	pid: number;
	started_at: string;
}

export interface Session {
	name: string;
	socketPath: string;
	tokenPath: string;
	metadata: SessionMetadata;
	alive: boolean;
}
