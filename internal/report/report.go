package report

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ba0f3/plane-cli/internal/api"
)

type Fetcher interface {
	ListObjects(context.Context, string, url.Values) ([]api.Object, error)
}

type Scope struct {
	Workspace api.Workspace
	Project   api.Object
}

type Filter struct {
	Since     *time.Time
	Until     *time.Time
	DateField string
	Assignee  string
	State     string
	Priority  string
}

type Item struct {
	Workspace      string
	WorkspaceName  string
	ProjectID      string
	Project        string
	ProjectName    string
	ID             string
	Identifier     string
	Name           string
	Priority       string
	StateID        string
	State          string
	StateGroup     string
	Assignees      []string
	AssigneeIDs    []string
	Point          float64
	StartDate      string
	TargetDate     string
	CreatedAt      string
	UpdatedAt      string
	CreatedTime    time.Time
	UpdatedTime    time.Time
	TargetTime     time.Time
	HasCreatedTime bool
	HasUpdatedTime bool
	HasTargetTime  bool
}

type Collector struct {
	Client      Fetcher
	Concurrency int
}

func (c Collector) Collect(ctx context.Context, scopes []Scope, filter Filter) ([]Item, error) {
	concurrency := c.Concurrency
	if concurrency <= 0 {
		concurrency = 6
	}
	type result struct {
		items []Item
		err   error
	}
	jobs := make(chan Scope)
	results := make(chan result, len(scopes))
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for scope := range jobs {
				items, err := c.collectProject(ctx, scope, filter)
				results <- result{items: items, err: err}
			}
		}()
	}
	go func() {
		defer close(results)
		for _, scope := range scopes {
			select {
			case jobs <- scope:
			case <-ctx.Done():
				close(jobs)
				wg.Wait()
				return
			}
		}
		close(jobs)
		wg.Wait()
	}()

	var all []Item
	for r := range results {
		if r.err != nil {
			return nil, r.err
		}
		all = append(all, r.items...)
	}
	sort.SliceStable(all, func(i, j int) bool {
		return all[i].UpdatedAt > all[j].UpdatedAt
	})
	return all, nil
}

func (c Collector) collectProject(ctx context.Context, scope Scope, filter Filter) ([]Item, error) {
	projectID := api.String(scope.Project, "id")
	if projectID == "" {
		return nil, fmt.Errorf("project missing id in workspace %s", scope.Workspace.Slug)
	}
	base := fmt.Sprintf("/api/v1/workspaces/%s/projects/%s", url.PathEscape(scope.Workspace.Slug), url.PathEscape(projectID))
	items, err := c.Client.ListObjects(ctx, base+"/work-items/", url.Values{"expand": {"assignees,state,labels"}})
	if err != nil {
		return nil, fmt.Errorf("list work items for %s/%s: %w", scope.Workspace.Slug, api.String(scope.Project, "name"), err)
	}
	states, _ := c.Client.ListObjects(ctx, base+"/states/", nil)
	members, _ := c.Client.ListObjects(ctx, fmt.Sprintf("/api/v1/workspaces/%s/members/", url.PathEscape(scope.Workspace.Slug)), nil)
	stateMap := make(map[string]api.Object, len(states))
	for _, state := range states {
		stateMap[api.String(state, "id")] = state
	}
	memberMap := make(map[string]string, len(members))
	for _, member := range members {
		id := api.String(member, "id")
		name := api.String(member, "display_name", "name", "email")
		if name == "" {
			first := api.String(member, "first_name")
			last := api.String(member, "last_name")
			name = strings.TrimSpace(first + " " + last)
		}
		if id != "" {
			memberMap[id] = name
		}
	}

	out := make([]Item, 0, len(items))
	for _, raw := range items {
		item := normalizeItem(scope, raw, stateMap, memberMap)
		if matches(item, filter) {
			out = append(out, item)
		}
	}
	return out, nil
}

func normalizeItem(scope Scope, raw api.Object, stateMap map[string]api.Object, memberMap map[string]string) Item {
	item := Item{
		Workspace:     scope.Workspace.Slug,
		WorkspaceName: scope.Workspace.Name,
		ProjectID:     api.String(scope.Project, "id"),
		Project:       api.String(scope.Project, "identifier"),
		ProjectName:   api.String(scope.Project, "name"),
		ID:            api.String(raw, "id"),
		Name:          api.String(raw, "name"),
		Priority:      api.String(raw, "priority"),
		StartDate:     api.String(raw, "start_date"),
		TargetDate:    api.String(raw, "target_date"),
		CreatedAt:     api.String(raw, "created_at"),
		UpdatedAt:     api.String(raw, "updated_at"),
		Point:         api.Number(raw, "point", "estimate_point"),
	}
	seq := api.String(raw, "sequence_id")
	if seq != "" && item.Project != "" {
		item.Identifier = item.Project + "-" + seq
	}

	stateID := api.String(raw, "state")
	if nested, ok := raw["state_detail"].(map[string]any); ok {
		item.StateID = api.String(api.Object(nested), "id")
		item.State = api.String(api.Object(nested), "name")
		item.StateGroup = api.String(api.Object(nested), "group")
	} else if state, ok := stateMap[stateID]; ok {
		item.StateID = stateID
		item.State = api.String(state, "name")
		item.StateGroup = api.String(state, "group")
	} else {
		item.StateID = stateID
		item.State = stateID
	}

	for _, key := range []string{"assignees", "assignee_details", "assignee_detail"} {
		v, ok := raw[key]
		if !ok || v == nil {
			continue
		}
		arr, ok := v.([]any)
		if !ok {
			continue
		}
		for _, entry := range arr {
			switch x := entry.(type) {
			case string:
				item.AssigneeIDs = append(item.AssigneeIDs, x)
				if n := memberMap[x]; n != "" {
					item.Assignees = append(item.Assignees, n)
				} else {
					item.Assignees = append(item.Assignees, x)
				}
			case map[string]any:
				obj := api.Object(x)
				id := api.String(obj, "id")
				name := api.String(obj, "display_name", "name", "email")
				if name == "" && id != "" {
					name = memberMap[id]
				}
				if id != "" {
					item.AssigneeIDs = append(item.AssigneeIDs, id)
				}
				if name != "" {
					item.Assignees = append(item.Assignees, name)
				}
			}
		}
		if len(item.Assignees) > 0 || len(item.AssigneeIDs) > 0 {
			break
		}
	}
	item.CreatedTime, item.HasCreatedTime = api.Time(raw, "created_at")
	item.UpdatedTime, item.HasUpdatedTime = api.Time(raw, "updated_at")
	item.TargetTime, item.HasTargetTime = api.Time(raw, "target_date")
	return item
}

func matches(item Item, filter Filter) bool {
	if filter.Priority != "" && !strings.EqualFold(item.Priority, filter.Priority) {
		return false
	}
	if filter.State != "" && !strings.EqualFold(item.State, filter.State) && !strings.EqualFold(item.StateGroup, filter.State) {
		return false
	}
	if filter.Assignee != "" {
		needle := strings.ToLower(filter.Assignee)
		found := false
		for _, name := range item.Assignees {
			if strings.Contains(strings.ToLower(name), needle) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	var t time.Time
	var ok bool
	switch strings.ToLower(filter.DateField) {
	case "created":
		t, ok = item.CreatedTime, item.HasCreatedTime
	case "target", "due":
		t, ok = item.TargetTime, item.HasTargetTime
	default:
		t, ok = item.UpdatedTime, item.HasUpdatedTime
	}
	if filter.Since != nil {
		if !ok || t.Before(*filter.Since) {
			return false
		}
	}
	if filter.Until != nil {
		if !ok || t.After(*filter.Until) {
			return false
		}
	}
	return true
}

func ParseTime(value string, now time.Time) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	if d, err := time.ParseDuration(value); err == nil {
		t := now.Add(-d)
		return &t, nil
	}
	if strings.HasSuffix(value, "d") {
		var days int
		if _, err := fmt.Sscanf(value, "%dd", &days); err == nil && days >= 0 {
			t := now.AddDate(0, 0, -days)
			return &t, nil
		}
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, value, now.Location()); err == nil {
			return &t, nil
		}
	}
	return nil, fmt.Errorf("invalid time %q; use RFC3339, YYYY-MM-DD, Go duration (24h), or Nd (7d)", value)
}
