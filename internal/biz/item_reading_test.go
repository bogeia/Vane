package biz

import "testing"

func TestItemReadingAvailabilityUsesDiscussionPublicationPolicy(t *testing.T) {
	usecase, repo, _, _, now := discussionFixture()
	if err := usecase.RunOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	valid := DiscussionRead{State: repo.states[1], Target: repo.targets[0], Pool: repo.pool[0], ReadAt: *now}
	for _, test := range []struct {
		name    string
		change  func(*DiscussionRead)
		visible bool
	}{
		{name: "valid cache", change: func(*DiscussionRead) {}, visible: true},
		{name: "failed refresh keeps verified evidence", change: func(r *DiscussionRead) { r.State.LastError = "refresh failed"; r.State.NextRefreshAt = r.ReadAt }, visible: true},
		{name: "expired confirmation", change: func(r *DiscussionRead) { r.ReadAt = r.ReadAt.Add(DiscussionFreshness) }},
		{name: "empty summary", change: func(r *DiscussionRead) { r.State.Points = nil }},
		{name: "changed event", change: func(r *DiscussionRead) { r.Target.Signature = "changed" }},
		{name: "changed pool", change: func(r *DiscussionRead) { r.Pool.Fingerprint = "changed" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			read := valid
			test.change(&read)
			item := CompleteItem(Item{HasBody: true, DiscussionRead: &read})
			if item.HasDiscussion != test.visible || item.HasDiscussion != (read.Summary() != nil) || !item.HasBody {
				t.Fatalf("availability inconsistent with detail: %+v", item)
			}
		})
	}
	if CompleteItem(Item{}).HasDiscussion {
		t.Fatal("missing cache marked readable")
	}
}
