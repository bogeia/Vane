package llm

import "github.com/bogeia/vane/internal/biz"

type factDTO struct {
	Title      string  `json:"title"`
	Subject    string  `json:"subject"`
	Action     string  `json:"action"`
	Object     string  `json:"object"`
	OccurredAt *string `json:"occurred_at"`
}

func (f factDTO) domain() biz.Fact {
	return biz.Fact{Title: f.Title, Subject: f.Subject, Action: f.Action, Object: f.Object, OccurredAt: f.OccurredAt}
}

type writingDTO struct {
	Roundup  bool     `json:"roundup"`
	Title    string   `json:"title"`
	Summary  string   `json:"summary"`
	Reason   string   `json:"reason"`
	Category string   `json:"category"`
	Tags     []string `json:"tags"`
	Subjects []string `json:"subjects"`
	Fact     *factDTO `json:"fact"`
}

func (w writingDTO) domain() biz.Writing {
	result := biz.Writing{Roundup: w.Roundup, Title: w.Title, Summary: w.Summary, Reason: w.Reason, Category: w.Category, Tags: w.Tags, Subjects: w.Subjects}
	if w.Fact != nil {
		f := w.Fact.domain()
		result.Fact = &f
	}
	return result
}

type storyCandidateDTO struct {
	StoryID    int64   `json:"story_id"`
	Title      string  `json:"title"`
	Summary    string  `json:"summary"`
	Similarity float64 `json:"similarity"`
}

func storyCandidatesDTO(values []biz.StoryCandidate) []storyCandidateDTO {
	if values == nil {
		return nil
	}
	result := make([]storyCandidateDTO, len(values))
	for i, v := range values {
		result[i] = storyCandidateDTO{v.StoryID, v.Title, v.Summary, v.Similarity}
	}
	return result
}
