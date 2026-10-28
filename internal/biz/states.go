package biz

const (
	StageNew               = "new"
	StagePrefiltered       = "prefiltered"
	StageAnalyzed          = "analyzed"
	StageDone              = "done"
	StageBlocked           = "blocked"
	StageFailed            = "failed"
	PrefilterPass          = "PASS"
	PrefilterUnknown       = "UNKNOWN"
	PrefilterBlock         = "BLOCK"
	RelationSameOccurrence = "SAME_OCCURRENCE"
	RelationSameStory      = "SAME_STORY"
	RelationUnrelated      = "UNRELATED"
	RelationRoundup        = "ROUNDUP"
	ViewAll                = "all"
	ViewSelected           = "selected"
)

const (
	TrendNew    = "new"
	TrendRising = "rising"
	LabelSelect = "select"
	LabelReject = "reject"
	LabelEither = "either"
)
