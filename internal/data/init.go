package data

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/bogeia/vane/internal/config"
)

func Migrate(ctx context.Context, d *Data) error {
	return d.ORM.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(784318610)").Error; err != nil {
			return err
		}
		// Remove the obsolete URL key before AutoMigrate creates the digest key.
		if tx.Migrator().HasConstraint(&blockedContentModel{}, "blocked_contents_pkey") {
			var columns string
			if err := tx.Raw("SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid = 'blocked_contents'::regclass AND conname = 'blocked_contents_pkey'").Scan(&columns).Error; err != nil {
				return err
			}
			if columns == "PRIMARY KEY (url)" {
				if err := tx.Migrator().DropConstraint(&blockedContentModel{}, "blocked_contents_pkey"); err != nil {
					return err
				}
			}
		}
		// The stories/items relationship is circular. Create every table before adding its foreign keys.
		structure := tx.Session(&gorm.Session{NewDB: true})
		structure.IgnoreRelationshipsWhenMigrating = true
		structure.DisableForeignKeyConstraintWhenMigrating = true
		if err := structure.AutoMigrate(&sourceModel{}, &storyModel{}, &itemModel{}, &reportModel{}, &fetchRecordModel{}, &blockedContentModel{}, &pushTargetModel{}, &pushDeliveryModel{}); err != nil {
			return err
		}
		if !tx.Migrator().HasConstraint(&blockedContentModel{}, "blocked_contents_pkey") {
			stmt := &gorm.Statement{DB: tx}
			if err := stmt.Parse(&blockedContentModel{}); err != nil {
				return err
			}
			columns := make([]clause.Column, len(stmt.Schema.PrimaryFieldDBNames))
			for i, name := range stmt.Schema.PrimaryFieldDBNames {
				columns[i] = clause.Column{Name: name}
			}
			if err := tx.Exec("ALTER TABLE ? ADD PRIMARY KEY ?", clause.Table{Name: stmt.Table}, columns).Error; err != nil {
				return err
			}
		}
		for _, obsolete := range []struct {
			model any
			name  string
		}{
			{&itemModel{}, "uni_items_url"},
			{&storyModel{}, "stories_representative_item_fk"},
			{&pushDeliveryModel{}, "push_deliveries_item_fk"},
		} {
			if tx.Migrator().HasConstraint(obsolete.model, obsolete.name) {
				if err := tx.Migrator().DropConstraint(obsolete.model, obsolete.name); err != nil {
					return err
				}
			}
		}
		for _, relation := range []struct {
			model any
			name  string
		}{
			{&pushDeliveryModel{}, "push_deliveries_target_fk"},
			{&pushDeliveryModel{}, "push_deliveries_story_fk"},
			{&pushDeliveryModel{}, "push_deliveries_item_membership_fk"},
			{&fetchRecordModel{}, "fetch_records_source_id_fkey"},
			{&blockedContentModel{}, "blocked_contents_source_id_fkey"},
			{&itemModel{}, "items_source_id_fkey"},
			{&itemModel{}, "items_story_id_fkey"},
			{&storyModel{}, "stories_representative_membership_fk"},
		} {
			if !tx.Migrator().HasConstraint(relation.model, relation.name) {
				if err := tx.Migrator().CreateConstraint(relation.model, relation.name); err != nil {
					return err
				}
			}
		}

		return nil
	})
}

func SyncSources(ctx context.Context, d *Data, sources []config.Source) error {
	return d.ORM.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&sourceModel{}).Where("TRUE").Update("enabled", false).Error; err != nil {
			return err
		}
		for _, source := range sources {
			values := map[string]any{"id": source.ID, "name": source.Name, "tier": source.Tier, "feed_url": source.FeedURL, "enabled": source.Enabled, "interval_minutes": source.IntervalMinutes}
			updates := clause.Assignments(map[string]any{
				"name": gorm.Expr("EXCLUDED.name"), "tier": gorm.Expr("EXCLUDED.tier"), "feed_url": gorm.Expr("EXCLUDED.feed_url"), "enabled": gorm.Expr("EXCLUDED.enabled"), "interval_minutes": gorm.Expr("EXCLUDED.interval_minutes"),
				"next_collect_at": gorm.Expr("CASE WHEN sources.feed_url <> EXCLUDED.feed_url THEN now() WHEN sources.interval_minutes <> EXCLUDED.interval_minutes AND sources.last_collected_at IS NOT NULL THEN GREATEST(sources.retry_not_before, sources.last_collected_at + EXCLUDED.interval_minutes * interval '1 minute') ELSE sources.next_collect_at END"),
			})
			for _, column := range []string{"etag", "last_modified", "last_error", "first_collected", "last_collected_at", "retry_not_before"} {
				reset := "''"
				if column == "first_collected" {
					reset = "false"
				}
				if column == "last_collected_at" || column == "retry_not_before" {
					reset = "NULL"
				}
				updates = append(updates, clause.Assignment{Column: clause.Column{Name: column}, Value: gorm.Expr("CASE WHEN sources.feed_url <> EXCLUDED.feed_url THEN " + reset + " ELSE sources." + column + " END")})
			}
			if err := tx.Model(&sourceModel{}).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: updates}).Create(values).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
