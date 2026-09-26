package issue

import (
	"strconv"
	"strings"

	"github.com/ba0f3/plane-cli/internal/api"
	md "github.com/ba0f3/plane-cli/internal/markdown"
	"github.com/ba0f3/plane-cli/pkg/plane"
)

func resolveIssue(client *api.Client, projectID, ref string) (*plane.Issue, error) {
	if seqID, err := strconv.Atoi(strings.TrimSpace(ref)); err == nil {
		return client.GetIssueBySequenceID(projectID, seqID)
	}

	if looksLikeUUID(ref) {
		return client.GetIssue(projectID, ref)
	}

	issue, err := client.GetIssue(projectID, ref)
	if err == nil {
		return issue, nil
	}

	return client.GetIssueByIdentifier(ref)
}

func resolveIssueID(client *api.Client, projectID, ref string) (string, error) {
	issue, err := resolveIssue(client, projectID, ref)
	if err != nil {
		return "", err
	}

	return issue.ID, nil
}

func looksLikeUUID(s string) bool {
	if len(s) != 36 {
		return false
	}

	for i, r := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if r != '-' {
				return false
			}
			continue
		}

		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}

	return true
}

func renderDescriptionHTML(input string) string {
	return md.RenderHTML(input)
}
