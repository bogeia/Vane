package data

import (
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

type sourceModel struct {
	IconVersion     string             `gorm:"type:text;not null;default:''"`
	IconInput       string             `gorm:"type:text;not null;default:''"`
	IconContent     []byte             `gorm:"type:bytea"`
	IconContentType string             `gorm:"type:text;not null;default:''"`
	ID              string             `gorm:"type:text;primaryKey"`
	Name            string             `gorm:"type:text;not null"`
	Tier            string             `gorm:"type:text;not null;check:sources_tier_check,tier IN ('official','media')"`
	FeedURL         string             `gorm:"type:text;not null"`
	Enabled         bool               `gorm:"not null"`
	IntervalMinutes int64              `gorm:"not null;check:sources_interval_minutes_check,interval_minutes > 0"`
	ETag            string             `gorm:"column:etag;type:text;not null;default:''"`
	LastModified    string             `gorm:"type:text;not null;default:''"`
	FirstCollected  bool               `gorm:"not null;default:false"`
	AlertStartedAt  *time.Time         `gorm:"type:timestamptz"`
	RetryNotBefore  *time.Time         `gorm:"type:timestamptz"`
	LastCollectedAt *time.Time         `gorm:"type:timestamptz"`
	NextCollectAt   pgtype.Timestamptz `gorm:"type:timestamptz;not null;default:'-infinity'::timestamptz"`
	LastError       string             `gorm:"type:text;not null;default:''"`
}

func (sourceModel) TableName() string { return "sources" }

type fetchRecordModel struct {
	ID        int64        `gorm:"primaryKey;generated:identity always"`
	SourceID  string       `gorm:"type:text;not null;index:fetch_records_source_time_idx,priority:1"`
	FetchedAt time.Time    `gorm:"type:timestamptz;not null;index:fetch_records_source_time_idx,priority:2"`
	Success   bool         `gorm:"not null"`
	Error     string       `gorm:"type:text;not null"`
	NewItems  int64        `gorm:"not null;check:fetch_records_new_items_check,new_items >= 0"`
	Source    *sourceModel `gorm:"foreignKey:SourceID;constraint:fetch_records_source_id_fkey,OnDelete:RESTRICT"`
}

func (fetchRecordModel) TableName() string { return "fetch_records" }

type storyModel struct {
	ID                   int64      `gorm:"primaryKey;generated:identity always"`
	Title                string     `gorm:"type:text;not null"`
	RepresentativeItemID *int64     `gorm:"index:stories_representative_item_idx"`
	CreatedAt            time.Time  `gorm:"type:timestamptz;not null;default:now();autoCreateTime:false"`
	Representative       *itemModel `gorm:"belongsTo;foreignKey:RepresentativeItemID,ID;references:ID,StoryID;constraint:stories_representative_membership_fk,OnDelete:RESTRICT"`
}

func (storyModel) TableName() string { return "stories" }

type itemModel struct {
	ID       int64  `gorm:"primaryKey;generated:identity always;uniqueIndex:items_id_story_key,priority:1;index:items_pending_idx,priority:2,where:stage = 'new' OR stage = 'prefiltered' OR stage = 'analyzed';index:items_public_idx,priority:2,sort:desc,where:stage = 'done'"`
	SourceID string `gorm:"type:text;not null;index:items_source_idx"`
	// Escape backslashes before bytea input parsing to hash the original URL bytes.
	URLHash       []byte             `gorm:"type:bytea;generated:sha256(replace(url, chr(92), chr(92)||chr(92))::bytea);->;not null;uniqueIndex:items_url_hash_key"`
	URL           string             `gorm:"type:text;not null"`
	OriginalTitle string             `gorm:"type:text;not null"`
	Body          string             `gorm:"type:text;not null;default:''"`
	ImageURL      string             `gorm:"type:text;not null;default:''"`
	PublishedAt   time.Time          `gorm:"type:timestamptz;not null;index:items_public_idx,priority:1,sort:desc,where:stage = 'done'"`
	DiscoveredAt  time.Time          `gorm:"type:timestamptz;not null;default:now()"`
	ProcessedAt   *time.Time         `gorm:"type:timestamptz"`
	Stage         string             `gorm:"type:text;not null;default:new;check:items_stage_check,stage IN ('new','prefiltered','analyzed','done','blocked','failed')"`
	Attempts      int64              `gorm:"not null;default:0;check:items_attempts_check,attempts >= 0"`
	NextAttemptAt pgtype.Timestamptz `gorm:"type:timestamptz;not null;default:now();index:items_pending_idx,priority:1,where:stage = 'new' OR stage = 'prefiltered' OR stage = 'analyzed'"`
	LastError     string             `gorm:"type:text;not null;default:''"`
	Prefilter     *string            `gorm:"type:text;check:items_prefilter_check,prefilter IN ('PASS','UNKNOWN','BLOCK')"`
	Score1        *int               `gorm:"type:bigint;check:items_score1_check,score1 BETWEEN 0 AND 100"`
	Score2        *int               `gorm:"type:bigint;check:items_score2_check,score2 BETWEEN 0 AND 100"`
	Title         string             `gorm:"type:text;not null;default:''"`
	Summary       string             `gorm:"type:text;not null;default:''"`
	Reason        string             `gorm:"type:text;not null;default:''"`
	Category      string             `gorm:"type:text;not null;default:''"`
	Tags          textArray          `gorm:"type:text[];not null;default:'{}'"`
	Topics        textArray          `gorm:"type:text[];not null;default:'{}';index:items_topics_idx,type:gin"`
	Subjects      textArray          `gorm:"type:text[];not null;default:'{}'"`
	Fact          json.RawMessage    `gorm:"type:jsonb"`
	Roundup       bool               `gorm:"not null;default:false"`
	Selected      bool               `gorm:"not null;default:false"`
	StoryID       *int64             `gorm:"index:items_story_idx;uniqueIndex:items_id_story_key,priority:2"`
	Source        *sourceModel       `gorm:"foreignKey:SourceID;constraint:items_source_id_fkey,OnDelete:RESTRICT"`
	Story         *storyModel        `gorm:"foreignKey:StoryID;constraint:items_story_id_fkey,OnDelete:RESTRICT"`
}

func (itemModel) TableName() string { return "items" }

type reportModel struct {
	PushedAt  *time.Time      `gorm:"type:timestamptz"`
	Date      string          `gorm:"type:date;primaryKey"`
	Content   json.RawMessage `gorm:"type:jsonb;not null"`
	CreatedAt time.Time       `gorm:"type:timestamptz;not null;default:now();autoCreateTime:false"`
}

func (reportModel) TableName() string { return "reports" }

type pushTargetModel struct {
	Name        string    `gorm:"type:text;primaryKey"`
	FirstSeenAt time.Time `gorm:"type:timestamptz;not null"`
}

func (pushTargetModel) TableName() string { return "push_targets" }

type pushDeliveryModel struct {
	TargetName    string           `gorm:"type:text;primaryKey"`
	StoryID       int64            `gorm:"primaryKey;autoIncrement:false;index:push_deliveries_story_idx;index:push_deliveries_item_story_idx,priority:2"`
	ItemID        int64            `gorm:"not null;index:push_deliveries_item_story_idx,priority:1"`
	Status        string           `gorm:"type:text;not null;check:push_deliveries_status_check,status IN ('sent','failed','abandoned')"`
	Attempts      int              `gorm:"not null;check:push_deliveries_attempts_check,attempts BETWEEN 1 AND 4"`
	NextAttemptAt time.Time        `gorm:"type:timestamptz;not null"`
	LastError     string           `gorm:"type:text;not null"`
	CreatedAt     time.Time        `gorm:"type:timestamptz;not null;autoCreateTime:false"`
	UpdatedAt     time.Time        `gorm:"type:timestamptz;not null;autoUpdateTime:false"`
	Target        *pushTargetModel `gorm:"foreignKey:TargetName;constraint:push_deliveries_target_fk,OnDelete:RESTRICT"`
	Story         *storyModel      `gorm:"foreignKey:StoryID;constraint:push_deliveries_story_fk,OnDelete:RESTRICT"`
	Item          *itemModel       `gorm:"belongsTo;foreignKey:ItemID,StoryID;references:ID,StoryID;constraint:push_deliveries_item_membership_fk,OnDelete:RESTRICT"`
}

func (pushDeliveryModel) TableName() string { return "push_deliveries" }

// Blocked contents remain private and are kept when rules later allow collection.
type blockedContentModel struct {
	URLHash        []byte       `gorm:"type:bytea;generated:sha256(replace(url, chr(92), chr(92)||chr(92))::bytea);->;primaryKey"`
	URL            string       `gorm:"type:text;not null"`
	SourceID       string       `gorm:"type:text;not null;index:blocked_contents_source_time_idx,priority:1"`
	OriginalTitle  string       `gorm:"type:text;not null"`
	FirstBlockedAt time.Time    `gorm:"type:timestamptz;not null;index:blocked_contents_source_time_idx,priority:2;index:blocked_contents_time_idx"`
	Source         *sourceModel `gorm:"foreignKey:SourceID;constraint:blocked_contents_source_id_fkey,OnDelete:RESTRICT"`
}

func (blockedContentModel) TableName() string { return "blocked_contents" }
