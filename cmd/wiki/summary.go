package wiki

import "github.com/ba0f3/plane-cli/internal/api"

var wikiContentFields = map[string]struct{}{
	"description_html":     {},
	"description_json":     {},
	"description_stripped": {},
	"description_markdown": {},
}

func summarizeWikiPages(pages []api.WikiPage) []api.WikiPage {
	summaries := make([]api.WikiPage, 0, len(pages))
	for _, page := range pages {
		summary := make(api.WikiPage, len(page))
		for key, value := range page {
			if _, isContent := wikiContentFields[key]; isContent {
				continue
			}
			summary[key] = value
		}
		summaries = append(summaries, summary)
	}
	return summaries
}
