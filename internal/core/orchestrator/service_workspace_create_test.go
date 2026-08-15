package orchestrator

import "testing"

func TestWorkspaceCreatePickerRootForGOOSWindowsUsesInitialPathVolume(t *testing.T) {
	got := workspaceCreatePickerRootForGOOS("windows", `E:\temp\demo`)
	if got != "E:/" {
		t.Fatalf("workspaceCreatePickerRootForGOOS(windows) = %q, want %q", got, "E:/")
	}
}

func TestWorkspaceCreatePickerRootForGOOSUnixUsesFilesystemRoot(t *testing.T) {
	got := workspaceCreatePickerRootForGOOS("linux", "/tmp/demo")
	if got != "/" {
		t.Fatalf("workspaceCreatePickerRootForGOOS(linux) = %q, want /", got)
	}
}

func TestWorkspacePickerPathsForGOOSWindowsUsesVolumeRootAsInitialWhenWorkspaceEmpty(t *testing.T) {
	root, initial := workspacePickerPathsForGOOS("windows", "", `E:\Users\demo`)
	if root != "E:/" || initial != "E:/" {
		t.Fatalf("workspacePickerPathsForGOOS(windows, empty) = (%q, %q), want (%q, %q)", root, initial, "E:/", "E:/")
	}
}

func TestWorkspacePickerPathsForGOOSWindowsUsesUNCShareAsRoot(t *testing.T) {
	root, initial := workspacePickerPathsForGOOS("windows", `\\fileserver\engineering\demo`, `C:\Users\demo`)
	if root != "//fileserver/engineering/" || initial != `\\fileserver\engineering\demo` {
		t.Fatalf("workspacePickerPathsForGOOS(windows, UNC) = (%q, %q)", root, initial)
	}
}

func TestWindowsPickerRootRecognizesForwardSlashUNCShare(t *testing.T) {
	if got := windowsPickerRoot("//fileserver/engineering/demo"); got != "//fileserver/engineering/" {
		t.Fatalf("windowsPickerRoot(UNC) = %q, want UNC share root", got)
	}
}

func TestWorkspacePickerPathsForGOOSUnixUsesFilesystemRootAsInitialWhenWorkspaceEmpty(t *testing.T) {
	root, initial := workspacePickerPathsForGOOS("linux", "", "")
	if root != "/" || initial != "/" {
		t.Fatalf("workspacePickerPathsForGOOS(linux, empty) = (%q, %q), want (%q, %q)", root, initial, "/", "/")
	}
}

func TestShouldResolveWorkspacePathOnHostWindowsKeepsSlashRootWorkspaceKeysLogical(t *testing.T) {
	if shouldResolveWorkspacePathOnHost("windows", "/data/dl/demo") {
		t.Fatal("expected slash-root workspace key to stay logical on windows")
	}
}

func TestShouldResolveWorkspacePathOnHostWindowsResolvesRelativeDotPaths(t *testing.T) {
	if !shouldResolveWorkspacePathOnHost("windows", "./demo") {
		t.Fatal("expected relative dot path to resolve on host")
	}
}

func TestShouldResolveWorkspacePathOnHostWindowsResolvesVolumePaths(t *testing.T) {
	if !shouldResolveWorkspacePathOnHost("windows", `D:\data\dl\demo`) {
		t.Fatal("expected volume path to resolve on host")
	}
}
