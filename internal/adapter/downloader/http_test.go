package downloader

import "testing"

func TestServerArtifactName(t *testing.T) {
	tests := []struct {
		name   string
		arch   string
		alpine bool
		want   string
	}{
		{name: "glibc x64", arch: "x64", alpine: false, want: "server-linux-x64"},
		{name: "glibc arm64", arch: "arm64", alpine: false, want: "server-linux-arm64"},
		{name: "alpine x64", arch: "x64", alpine: true, want: "server-linux-alpine"},
		{name: "alpine arm64", arch: "arm64", alpine: true, want: "server-alpine-arm64"},
		{name: "alpine unknown arch falls back", arch: "armhf", alpine: true, want: "server-linux-armhf"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := serverArtifactName(tt.arch, tt.alpine)
			if got != tt.want {
				t.Fatalf("serverArtifactName(%q, alpine=%v) = %q, want %q", tt.arch, tt.alpine, got, tt.want)
			}
		})
	}
}

func TestIsAlpineOSRelease(t *testing.T) {
	tests := []struct {
		name string
		data string
		want bool
	}{
		{name: "alpine plain", data: "NAME=Alpine Linux\nID=alpine\n", want: true},
		{name: "alpine quoted", data: "NAME=Alpine Linux\nID=\"alpine\"\n", want: true},
		{name: "ubuntu", data: "NAME=Ubuntu\nID=ubuntu\n", want: false},
		{name: "no id", data: "NAME=Unknown\n", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isAlpineOSRelease([]byte(tt.data))
			if got != tt.want {
				t.Fatalf("isAlpineOSRelease() = %v, want %v", got, tt.want)
			}
		})
	}
}
