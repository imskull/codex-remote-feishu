package orchestrator

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestWorkspacePickerPathsMissingHintUsesNearestExistingParent(t *testing.T) {
	parent := t.TempDir()
	missing := filepath.Join(parent, "deleted", "workspace")
	root, initial := workspacePickerPaths(missing)
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		t.Fatal(err)
	}
	if !samePath(initial, resolvedParent) || !pathWithinRoot(root, initial) {
		t.Fatalf("picker paths = (%q, %q), want parent %q within root", root, initial, resolvedParent)
	}
}

func TestWorkspacePickerPathsKeepsExistingDirectoryAndRejectsFileHint(t *testing.T) {
	directory := t.TempDir()
	_, initial := workspacePickerPaths(directory)
	if !samePath(initial, directory) {
		t.Fatalf("existing directory changed to %q", initial)
	}
	file := filepath.Join(directory, "file.txt")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	root, initial := workspacePickerPaths(file)
	if _, _, err := resolvePathPickerInitialState(root, pathPickerModeDirectory, initial); err == nil {
		t.Fatal("file hint must remain invalid for directory selection")
	}
	if _, _, err := resolvePathPickerInitialState(root, pathPickerModeDirectory, filepath.Join(directory, "missing")); err == nil {
		t.Fatal("explicit picker targets must still reject missing paths")
	}
}

func TestWorkspacePathPickerStaleDraftStillOpensAndCanReturn(t *testing.T) {
	for _, field := range []string{control.FeishuTargetPickerPathFieldLocalDirectory, control.FeishuTargetPickerPathFieldGitParentDir} {
		t.Run(field, func(t *testing.T) {
			now := time.Now()
			svc := newServiceForTest(&now)
			parent := t.TempDir()
			missing := filepath.Join(parent, "deleted")
			if err := os.Mkdir(missing, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(missing); err != nil {
				t.Fatal(err)
			}
			surface := svc.ensureSurface(control.Action{SurfaceSessionID: "surface-1", ActorUserID: "user-1"})
			record := &activeTargetPickerRecord{PickerID: "owner-1", LocalDirectoryPath: missing, GitParentDir: missing}
			svc.setActiveTargetPicker(surface, record)
			view := singlePathPickerEvent(t, svc.openTargetPickerAddWorkspacePathPicker(surface, record, field))
			if !samePath(view.CurrentPath, parent) || !view.CanConfirm || view.CancelLabel != "返回" {
				t.Fatalf("expected usable parent picker with return action, got %#v", view)
			}
			if record.LocalDirectoryPath != missing || record.GitParentDir != missing {
				t.Fatal("opening the picker must not overwrite the retained draft")
			}
			returned := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
				Kind: control.ActionPathPickerCancel, SurfaceSessionID: "surface-1", ActorUserID: "user-1", PickerID: view.PickerID,
			}))
			if returned.PickerID != record.PickerID || record.LocalDirectoryPath != missing || record.GitParentDir != missing {
				t.Fatal("return must restore the owner without changing its draft")
			}
		})
	}
}
