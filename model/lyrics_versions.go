package model

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// LyricVersion keeps the original format and content of a community lyric contribution.
type LyricVersion struct {
	Name      string `json:"name"`
	Format    string `json:"format"`
	Kind      string `json:"kind"`
	Language  string `json:"language"`
	Content   string `json:"content"`
	IsDefault bool   `json:"is_default"`
}

func parseLyricVersions(contents string) (LyricList, error) {
	var document struct {
		Versions []LyricVersion `json:"versions"`
	}
	if err := json.Unmarshal([]byte(contents), &document); err != nil {
		return nil, err
	}
	slices.SortStableFunc(document.Versions, func(a, b LyricVersion) int {
		if a.IsDefault == b.IsDefault {
			return 0
		}
		if a.IsDefault {
			return -1
		}
		return 1
	})
	var result LyricList
	for _, version := range document.Versions {
		var parsed LyricList
		if version.Format == "text" {
			lyric := Lyrics{Lang: version.Language}
			for line := range strings.SplitSeq(version.Content, "\n") {
				lyric.Line = append(lyric.Line, Line{Value: line})
			}
			parsed = LyricList{lyric}
		} else {
			if !slices.Contains([]string{"lrc", "elrc", "ttml"}, version.Format) {
				return nil, fmt.Errorf("unsupported lyric format: %s", version.Format)
			}
			var err error
			parsed, err = ParseLyrics(context.Background(), "."+version.Format, version.Language, []byte(version.Content))
			if err != nil {
				return nil, fmt.Errorf("parsing lyric version %q: %w", version.Name, err)
			}
		}
		for _, lyric := range parsed {
			if lyric.DisplayTitle == "" {
				lyric.DisplayTitle = version.Name
			}
			if version.Kind != "" && version.Kind != LyricKindMain {
				lyric.Kind = version.Kind
			}
			result = append(result, lyric)
		}
	}
	return result, nil
}
