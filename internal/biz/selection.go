package biz

import "github.com/bogeia/vane/internal/biz/taxonomy"

// Selection applies the source-tier threshold after two independent scores.
func Selection(tier string, score1, score2 int, thresholds Thresholds) (bool, int) {
	threshold, known := map[string]int{taxonomy.TierOfficial: thresholds.Official, taxonomy.TierMedia: thresholds.Media}[tier]
	return known && score1+score2 >= 2*threshold, AverageScore(score1, score2)
}

func AverageScore(first, second int) int { return (first + second) / 2 }
