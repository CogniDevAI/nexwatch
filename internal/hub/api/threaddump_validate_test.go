package api

import "testing"

func TestIsDumpablePID(t *testing.T) {
	procs := []processEntry{
		{PID: 100, Name: "java", Cmdline: "/usr/bin/java -jar app.jar"},
		{PID: 101, Name: "/opt/jdk/bin/java", Cmdline: "/opt/jdk/bin/java -jar app.jar"},
		{PID: 102, Name: "jsvc", Cmdline: "/usr/bin/jsvc -user tomcat"},
		{PID: 103, Name: "run-app.sh", Cmdline: "/bin/sh /opt/app/run-app.sh --start-java-service"},
		{PID: 104, Name: "nginx", Cmdline: "/usr/sbin/nginx -g daemon off;"},
		{PID: 105, Name: "sshd", Cmdline: "/usr/sbin/sshd -D"},
	}

	tests := []struct {
		name      string
		procs     []processEntry
		pid       int
		wantOK    bool
		wantEmpty bool // when true, assert reason is non-empty on failure
	}{
		{
			name:   "plain java process is dumpable",
			procs:  procs,
			pid:    100,
			wantOK: true,
		},
		{
			name:   "java with full path base name is dumpable",
			procs:  procs,
			pid:    101,
			wantOK: true,
		},
		{
			name:   "jsvc process is dumpable",
			procs:  procs,
			pid:    102,
			wantOK: true,
		},
		{
			name:   "wrapper script whose cmdline mentions java is dumpable",
			procs:  procs,
			pid:    103,
			wantOK: true,
		},
		{
			name:      "non-jvm process is rejected",
			procs:     procs,
			pid:       104,
			wantOK:    false,
			wantEmpty: true,
		},
		{
			name:      "unrelated system process is rejected",
			procs:     procs,
			pid:       105,
			wantOK:    false,
			wantEmpty: true,
		},
		{
			name:      "unknown pid is rejected",
			procs:     procs,
			pid:       9999,
			wantOK:    false,
			wantEmpty: true,
		},
		{
			name:      "empty snapshot rejects any pid",
			procs:     nil,
			pid:       100,
			wantOK:    false,
			wantEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, reason := isDumpablePID(tt.procs, tt.pid)
			if ok != tt.wantOK {
				t.Fatalf("isDumpablePID(pid=%d) ok = %v, want %v (reason=%q)", tt.pid, ok, tt.wantOK, reason)
			}
			if !ok && tt.wantEmpty && reason == "" {
				t.Fatalf("isDumpablePID(pid=%d) expected a non-empty reason on rejection", tt.pid)
			}
			if ok && reason != "" {
				t.Fatalf("isDumpablePID(pid=%d) unexpected reason on success: %q", tt.pid, reason)
			}
		})
	}
}

func TestLooksLikeJVM(t *testing.T) {
	tests := []struct {
		name    string
		procN   string
		cmdline string
		want    bool
	}{
		{name: "bare java name", procN: "java", cmdline: "java -jar app.jar", want: true},
		{name: "absolute path to java binary", procN: "/usr/lib/jvm/bin/java", cmdline: "", want: true},
		{name: "jsvc name", procN: "jsvc", cmdline: "", want: true},
		{name: "cmdline mentions java", procN: "start.sh", cmdline: "/bin/sh start.sh --exec java -jar x.jar", want: true},
		{name: "unrelated process", procN: "nginx", cmdline: "nginx -g daemon off;", want: false},
		{name: "empty everything", procN: "", cmdline: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := looksLikeJVM(tt.procN, tt.cmdline)
			if got != tt.want {
				t.Fatalf("looksLikeJVM(%q, %q) = %v, want %v", tt.procN, tt.cmdline, got, tt.want)
			}
		})
	}
}
