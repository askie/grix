package handler

import "testing"

func TestSplitCompleteSentences(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		wantSents []string
		wantCons  int // 已消费字节数（落在最后一个句末标点后）
	}{
		{"empty", "", nil, 0},
		{"no_ender", "今天天气不错", nil, 0},
		{"one_full", "今天天气不错。", []string{"今天天气不错。"}, len("今天天气不错。")},
		{"full_plus_partial", "你好。我想问一下", []string{"你好。"}, len("你好。")},
		{"two_full", "第一句。第二句！", []string{"第一句。", "第二句！"}, len("第一句。第二句！")},
		{"mixed_punct", "在吗？嗯，好的。还有", []string{"在吗？", "嗯，好的。"}, len("在吗？嗯，好的。")},
		{"newline_ender", "第一行\n剩下", []string{"第一行"}, len("第一行\n")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sents, cons := splitCompleteSentences(c.in)
			if cons != c.wantCons {
				t.Fatalf("consumed=%d want %d (in=%q)", cons, c.wantCons, c.in)
			}
			if len(sents) != len(c.wantSents) {
				t.Fatalf("sentences=%v want %v", sents, c.wantSents)
			}
			for i := range sents {
				if sents[i] != c.wantSents[i] {
					t.Fatalf("sentence[%d]=%q want %q", i, sents[i], c.wantSents[i])
				}
			}
			// consumed 必须落在 UTF-8 边界且可安全切片
			_ = c.in[cons:]
		})
	}
}
