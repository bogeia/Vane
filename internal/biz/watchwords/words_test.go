package watchwords

import (
	"reflect"
	"testing"
)

func TestMatchWords(t *testing.T) {
	for _, tt := range []struct {
		name            string
		groups          [][]string
		title, original string
		want            bool
		hits            []string
	}{
		{"普通词大小写", [][]string{{"Claude", "GPT"}}, "CLAUDE 发布", "", true, []string{"Claude"}},
		{"两标题必须词", [][]string{{"Claude", "+发布"}}, "新发布", "Claude update", true, []string{"Claude", "发布"}},
		{"所有必须词", [][]string{{"Claude", "+发布", "+模型"}}, "Claude发布", "", false, nil},
		{"合并命中组", [][]string{{"Claude", "!招聘"}, {"模型", "+发布"}}, "Claude招聘发布模型", "", true, []string{"模型", "发布"}},
		{"缺少必须词", [][]string{{"Claude", "+发布"}}, "Claude", "", false, nil},
		{"排除词", [][]string{{"Claude", "!招聘"}}, "Claude招聘", "", false, nil},
		{"多组", [][]string{{"GPT"}, {"智能体"}}, "智能体", "", true, []string{"智能体"}},
		{"正则边界", [][]string{{`/\bai\b/`}}, "training", "", false, nil},
		{"正则大小写", [][]string{{`/\bai\b/`}}, "", "AI update", true, []string{`/\bai\b/`}},
		{"跨标题不拼接", [][]string{{"Claude发布"}}, "Claude", "发布", false, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, hits := MatchWords(tt.groups, tt.title, tt.original)
			if got != tt.want || !reflect.DeepEqual(hits, tt.hits) {
				t.Fatalf("got %v %v want %v %v", got, hits, tt.want, tt.hits)
			}
		})
	}
}
