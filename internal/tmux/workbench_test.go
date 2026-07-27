package tmux

import (
	"reflect"
	"testing"
)

func TestNewScaffoldSessionBuildsArgs(t *testing.T) {
	r := &recordingRunner{out: "@0\n"}
	tm := &realTmux{run: r}

	id, err := tm.NewScaffoldSession("mysess", "/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := [][]string{{"new-session", "-d", "-s", "mysess", "-c", "/repo", "-P", "-F", "#{window_id}"}}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("args = %v, want %v", r.calls, want)
	}
	if id != "@0" {
		t.Fatalf("id = %q, want @0", id)
	}
}

func TestLiveWorkbenchWindowsBuildsArgsAndParses(t *testing.T) {
	r := &recordingRunner{out: "dev\t@1\n\t@2\nlogs\t@3\n"}
	tm := &realTmux{run: r}

	got, err := tm.LiveWorkbenchWindows("sess")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := [][]string{{"list-windows", "-t", "sess", "-F", "#{@pop_wb_window}\t#{window_id}"}}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("args = %v, want %v", r.calls, want)
	}
	// The unstamped window (empty identity before the tab) is skipped.
	wantMap := map[string]string{"dev": "@1", "logs": "@3"}
	if !reflect.DeepEqual(got, wantMap) {
		t.Fatalf("windows = %v, want %v", got, wantMap)
	}
}

func TestLivePaneIdentitiesBuildsArgsAndParses(t *testing.T) {
	r := &recordingRunner{out: "vim\t%1\n\t%2\nclaude\t%3\n"}
	tm := &realTmux{run: r}

	names, fallback, err := tm.LivePaneIdentities("@1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := [][]string{{"list-panes", "-t", "@1", "-F", "#{@pop_pane}\t#{pane_id}"}}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("args = %v, want %v", r.calls, want)
	}
	if fallback != "%1" {
		t.Fatalf("fallback = %q, want %%1", fallback)
	}
	wantMap := map[string]string{"vim": "%1", "claude": "%3"}
	if !reflect.DeepEqual(names, wantMap) {
		t.Fatalf("names = %v, want %v", names, wantMap)
	}
}

func TestStampWorkbenchWindowBuildsArgs(t *testing.T) {
	r := &recordingRunner{}
	tm := &realTmux{run: r}

	if err := tm.StampWorkbenchWindow("sess:work", "work"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := [][]string{{"set-option", "-w", "-t", "sess:work", "@pop_wb_window", "work"}}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("args = %v, want %v", r.calls, want)
	}
}

func TestDisableAutomaticRenameBuildsArgs(t *testing.T) {
	r := &recordingRunner{}
	tm := &realTmux{run: r}

	if err := tm.DisableAutomaticRename("sess:work"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := [][]string{{"set-option", "-w", "-t", "sess:work", "automatic-rename", "off"}}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("args = %v, want %v", r.calls, want)
	}
}

func TestStampPaneBuildsArgs(t *testing.T) {
	r := &recordingRunner{}
	tm := &realTmux{run: r}

	if err := tm.StampPane("%7", "server"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := [][]string{{"set-option", "-p", "-t", "%7", "@pop_pane", "server"}}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("args = %v, want %v", r.calls, want)
	}
}

func TestWindowSizeTargetsWindowAndParses(t *testing.T) {
	r := &recordingRunner{out: "120\t40\n"}
	tm := &realTmux{run: r}

	w, h, err := tm.WindowSize("%0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The dimension read must target the built window's pane via -t, never be
	// untargeted (regression: a detached session is 80x24 on the client).
	want := [][]string{{"display-message", "-t", "%0", "-p", "#{window_width}\t#{window_height}"}}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("args = %v, want %v", r.calls, want)
	}
	if w != 120 || h != 40 {
		t.Fatalf("size = %dx%d, want 120x40", w, h)
	}
}

func TestResizePaneBuildsArgs(t *testing.T) {
	tests := []struct {
		name       string
		horizontal bool
		wantFlag   string
	}{
		{"width", true, "-x"},
		{"height", false, "-y"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &recordingRunner{}
			tm := &realTmux{run: r}
			if err := tm.ResizePane("%1", tt.horizontal, 20); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			want := [][]string{{"resize-pane", "-t", "%1", tt.wantFlag, "20"}}
			if !reflect.DeepEqual(r.calls, want) {
				t.Fatalf("args = %v, want %v", r.calls, want)
			}
		})
	}
}

func TestRespawnPaneBuildsArgs(t *testing.T) {
	r := &recordingRunner{}
	tm := &realTmux{run: r}

	if err := tm.RespawnPane("%1", "/repo/api"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := [][]string{{"respawn-pane", "-c", "/repo/api", "-t", "%1", "-k"}}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("args = %v, want %v", r.calls, want)
	}
}

func TestSplitPaneBuildsArgs(t *testing.T) {
	tests := []struct {
		name string
		spec SplitSpec
		want []string
	}{
		{
			name: "forward horizontal",
			spec: SplitSpec{Target: "%1", Horizontal: true, Dir: "/repo"},
			want: []string{"split-window", "-h", "-t", "%1", "-P", "-F", "#{pane_id}", "-c", "/repo"},
		},
		{
			name: "before vertical",
			spec: SplitSpec{Target: "%2", Horizontal: false, Before: true, Dir: "/repo"},
			want: []string{"split-window", "-v", "-b", "-t", "%2", "-P", "-F", "#{pane_id}", "-c", "/repo"},
		},
		{
			name: "with percentage",
			spec: SplitSpec{Target: "%0", Horizontal: false, Percent: 50, Dir: "/tmp"},
			want: []string{"split-window", "-v", "-t", "%0", "-p", "50", "-P", "-F", "#{pane_id}", "-c", "/tmp"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &recordingRunner{out: "%9\n"}
			tm := &realTmux{run: r}
			id, err := tm.SplitPane(tt.spec)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(r.calls, [][]string{tt.want}) {
				t.Fatalf("args = %v, want %v", r.calls, [][]string{tt.want})
			}
			if id != "%9" {
				t.Fatalf("id = %q, want %%9", id)
			}
		})
	}
}

func TestKillWindowBuildsArgs(t *testing.T) {
	r := &recordingRunner{}
	tm := &realTmux{run: r}

	if err := tm.KillWindow("@0"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := [][]string{{"kill-window", "-t", "@0"}}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("args = %v, want %v", r.calls, want)
	}
}

func TestSelectWindowTargetBuildsArgs(t *testing.T) {
	r := &recordingRunner{}
	tm := &realTmux{run: r}

	if err := tm.SelectWindowTarget("@1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := [][]string{{"select-window", "-t", "@1"}}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("args = %v, want %v", r.calls, want)
	}
}
