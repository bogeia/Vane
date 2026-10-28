package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

func TestEvalExportCommandWritesOnlyEligibleGoldItems(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	cfg := loadTestConfig(t, databaseURL)
	app := assembleTestApp(t, cfg)
	now := time.Now().UTC().Truncate(time.Microsecond)
	body := "正文包含中文、\"引号\"。\n第二行保留换行。"
	for _, row := range []struct {
		title, stage      string
		score1, score2    any
		roundup, selected bool
		discovered        time.Time
	}{
		{"selected", "done", 80, 90, false, true, now.Add(-time.Hour)},
		{"rejected", "done", 40, 50, false, false, now.Add(-time.Hour)},
		{"roundup", "done", 90, 90, true, true, now.Add(-time.Hour)},
		{"unfinished", "new", 80, 90, false, true, now.Add(-time.Hour)},
		{"missing score1", "done", nil, 90, false, true, now.Add(-time.Hour)},
		{"missing score2", "done", 90, nil, false, true, now.Add(-time.Hour)},
		{"old", "done", 90, 90, false, true, now.AddDate(0, 0, -8)},
	} {
		_, err := testDB(t, app.Data).ExecContext(t.Context(), `INSERT INTO items(source_id,url,original_title,title,body,published_at,discovered_at,stage,score1,score2,roundup,selected) VALUES($1,$2,$3,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, cfg.Sources[0].ID, "https://example.test/"+row.title, row.title, body, now, row.discovered, row.stage, row.score1, row.score2, row.roundup, row.selected)
		if err != nil {
			t.Fatal(err)
		}
	}
	configPath := writeSubprocessConfig(t, databaseURL, "127.0.0.1:8080", cfg.Web.Dir)
	out := filepath.Join(t.TempDir(), "gold.yaml")
	cmd := exec.CommandContext(t.Context(), buildVane(t), "eval", "export", "--conf", configPath, "--out", out)
	output, err := cmd.Output()
	if err != nil {
		t.Fatal("eval export command failed (output suppressed to protect configuration)")
	}
	if len(output) != 0 {
		t.Fatalf("export unexpectedly wrote stdout: %q", output)
	}
	content, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var gold struct {
		Items []struct {
			ID            int64     `yaml:"id"`
			URL           string    `yaml:"url"`
			Tier          string    `yaml:"tier"`
			Source        string    `yaml:"source"`
			Title         string    `yaml:"title"`
			OriginalTitle string    `yaml:"original_title"`
			PublishedAt   time.Time `yaml:"published_at"`
			Score1        int       `yaml:"score1"`
			Score2        int       `yaml:"score2"`
			Label         string    `yaml:"label"`
			Body          string    `yaml:"body"`
		} `yaml:"items"`
	}
	if err := yaml.Unmarshal(content, &gold); err != nil {
		t.Fatal(err)
	}
	if len(gold.Items) != 2 {
		t.Fatalf("exported %d items, want two", len(gold.Items))
	}
	for _, item := range gold.Items {
		wantLabel := "reject"
		wantScores := []int{40, 50}
		if item.Title == "selected" {
			wantLabel = "select"
			wantScores = []int{80, 90}
		} else if item.Title != "rejected" {
			t.Fatalf("ineligible item exported: %s", item.Title)
		}
		if item.ID == 0 || item.URL != "https://example.test/"+item.Title || item.Tier != cfg.Sources[0].Tier || item.Source != cfg.Sources[0].Name || item.OriginalTitle != item.Title || !item.PublishedAt.Equal(now) || item.Score1 != wantScores[0] || item.Score2 != wantScores[1] || item.Label != wantLabel || item.Body != body {
			t.Fatalf("unexpected exported item: %+v", item)
		}
	}
	var document yaml.Node
	if err := yaml.Unmarshal(content, &document); err != nil {
		t.Fatal(err)
	}
	for _, item := range document.Content[0].Content[1].Content {
		var keys []string
		for i := 0; i < len(item.Content); i += 2 {
			keys = append(keys, item.Content[i].Value)
		}
		if !slices.Equal(keys, []string{"id", "url", "tier", "source", "title", "original_title", "published_at", "score1", "score2", "label", "body"}) {
			t.Fatalf("field order=%v", keys)
		}
	}
}

func TestEvalExportLimitsItemsAndProtectsExistingFiles(t *testing.T) {
	databaseURL := testDatabaseURL(t)
	resetTestSchema(t, databaseURL)
	cfg := loadTestConfig(t, databaseURL)
	app := assembleTestApp(t, cfg)
	if _, err := testDB(t, app.Data).ExecContext(t.Context(), `INSERT INTO items(source_id,url,original_title,title,published_at,stage,score1,score2) SELECT $1,'https://example.test/sample/'||n,'Original '||n,'中文标题 '||n,now(),'done',80,90 FROM generate_series(1,5) n`, cfg.Sources[0].ID); err != nil {
		t.Fatal(err)
	}
	configPath := writeSubprocessConfig(t, databaseURL, "127.0.0.1:8080", cfg.Web.Dir)
	binary := buildVane(t)
	out := filepath.Join(t.TempDir(), "gold.yaml")
	command := exec.CommandContext(t.Context(), binary, "eval", "export", "--conf", configPath, "--out", out, "--n", "2", "--days", "1")
	if err := command.Run(); err != nil {
		t.Fatal("sampled export failed")
	}
	before, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var gold struct {
		Items []map[string]any `yaml:"items"`
	}
	if err := yaml.Unmarshal(before, &gold); err != nil {
		t.Fatal(err)
	}
	if len(gold.Items) != 2 || gold.Items[0]["id"] == gold.Items[1]["id"] {
		t.Fatalf("sampled items=%v, want two distinct items", gold.Items)
	}
	if info, err := os.Stat(out); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("gold file permissions=%v err=%v", info, err)
	}
	command = exec.CommandContext(t.Context(), binary, "eval", "export", "--conf", configPath, "--out", out)
	if err := command.Run(); err == nil {
		t.Fatal("existing gold file was accepted")
	}
	after, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("existing gold file was changed")
	}
	for _, args := range [][]string{
		{"--days", "0"}, {"--days", "-1"}, {"--n", "0"}, {"--n", "-1"}, {"extra"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			target := filepath.Join(t.TempDir(), "invalid.yaml")
			base := []string{"eval", "export", "--conf", configPath, "--out", target}
			command := exec.CommandContext(t.Context(), binary, append(base, args...)...)
			if err := command.Run(); err == nil {
				t.Fatalf("accepted args %v", args)
			}
			if _, err := os.Stat(target); !os.IsNotExist(err) {
				t.Fatalf("invalid export left output: %v", err)
			}
		})
	}
	for _, args := range [][]string{{"eval", "export"}, {"eval", "export", "--conf", configPath}, {"eval", "export", "--out", filepath.Join(t.TempDir(), "missing-conf.yaml")}} {
		if err := exec.CommandContext(t.Context(), binary, args...).Run(); err == nil {
			t.Fatalf("accepted missing required flag: %v", args)
		}
	}
}
