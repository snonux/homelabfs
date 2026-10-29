package daemoncli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// behaviorLayout is private to the disposable command. Each existing root must
// be owned/exclusive; markers bind all three and prevent mixing foreign roots.
type behaviorLayout struct {
	FormatVersion int    `json:"format_version"`
	DataRoot      string `json:"data_root"`
	JournalRoot   string `json:"journal_root"`
	ControlRoot   string `json:"control_root"`
	NodeID        string `json:"node_id"`
	VolumeID      string `json:"volume_id"`
}

func sourcePath(c Config) string {
	if c.layout != nil {
		return filepath.Join(c.layout.JournalRoot, "volume", "source")
	}
	return filepath.Join(c.VolumeDir, "source")
}
func volumePaths(c Config) []string {
	paths := []string{sourcePath(c), filepath.Join(c.VolumeDir, "reservations"), filepath.Join(c.VolumeDir, "workspaces")}
	if c.layout != nil {
		paths = append(paths, filepath.Join(c.layout.JournalRoot, "volume", "workspaces"), filepath.Join(c.layout.ControlRoot, "workspaces"))
	}
	return paths
}
func workspacePaths(c Config, id string) (paths, parents []string) {
	data := filepath.Join(c.VolumeDir, "workspaces", id)
	journal, progress := data, data
	parents = []string{data}
	if c.layout != nil {
		journal = filepath.Join(c.layout.JournalRoot, "volume", "workspaces", id)
		progress = filepath.Join(c.layout.ControlRoot, "workspaces", id)
		parents = append(parents, journal, progress)
	}
	return []string{sourcePath(c), filepath.Join(c.VolumeDir, "reservations"), filepath.Join(data, "data"), filepath.Join(journal, "journal"), filepath.Join(progress, "progress")}, parents
}
func (l behaviorLayout) roots() []string { return []string{l.DataRoot, l.JournalRoot, l.ControlRoot} }
func (l behaviorLayout) config(mountpoint string) Config {
	return Config{FormatVersion: formatVersion, NodeID: l.NodeID, VolumeID: l.VolumeID, ControlDir: filepath.Join(l.ControlRoot, "node"), VolumeDir: filepath.Join(l.DataRoot, "volume"), Mountpoint: mountpoint, layout: &l}
}
func (l behaviorLayout) validate(mountpoint string, initialize bool) error {
	paths := append(l.roots(), mountpoint)
	for i, p := range paths {
		if err := privateDir(p); err != nil {
			return err
		}
		for _, other := range paths[:i] {
			if containsPath(p, other) || containsPath(other, p) {
				return fmt.Errorf("fixture roots/mount overlap")
			}
		}
		if initialize || i == 3 {
			entries, err := os.ReadDir(p)
			if err != nil {
				return err
			}
			if len(entries) != 0 {
				return fmt.Errorf("fixture initialization/mount requires empty root: %s", p)
			}
		}
	}
	return nil
}
func prepareBehavior(ctx context.Context, l behaviorLayout, mountpoint string, initialize bool) (Config, error) {
	if err := l.validate(mountpoint, initialize); err != nil {
		return Config{}, err
	}
	if initialize {
		node, err := initNode(ctx, filepath.Join(l.ControlRoot, "node"))
		if err != nil {
			return Config{}, err
		}
		volume, err := freshID()
		if err != nil {
			return Config{}, err
		}
		l.FormatVersion, l.NodeID, l.VolumeID = formatVersion, node.NodeID, volume.String()
		// Mark every root before any source bytes are created. Interrupted setup
		// remains preserved and cannot be silently initialized over on a retry.
		for _, root := range l.roots() {
			if err := writeNewJSON(filepath.Join(root, "fixture.json"), l); err != nil {
				return Config{}, err
			}
		}
		if err := createPrivate(filepath.Join(l.JournalRoot, "volume")); err != nil {
			return Config{}, err
		}
		c := l.config(mountpoint)
		if _, err := initVolume(ctx, c); err != nil {
			return Config{}, err
		}
		return c, nil
	}
	var saved behaviorLayout
	if err := readJSON(filepath.Join(l.ControlRoot, "fixture.json"), &saved); err != nil {
		return Config{}, err
	}
	if saved.FormatVersion != formatVersion || saved.DataRoot != l.DataRoot || saved.JournalRoot != l.JournalRoot || saved.ControlRoot != l.ControlRoot {
		return Config{}, fmt.Errorf("fixture root mapping mismatch")
	}
	for _, root := range l.roots() {
		var marker behaviorLayout
		if err := readJSON(filepath.Join(root, "fixture.json"), &marker); err != nil {
			return Config{}, err
		}
		if marker != saved {
			return Config{}, fmt.Errorf("foreign or changed fixture root: %s", root)
		}
	}
	c := saved.config(mountpoint)
	if _, err := inspectVolume(c); err != nil {
		return Config{}, err
	}
	return c, nil
}
