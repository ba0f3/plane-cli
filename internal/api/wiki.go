package api

import (
	"fmt"
	"net/url"
)

type WikiPage map[string]interface{}

func (c *Client) ListWikiPages(archived bool, updatedAfter string) ([]WikiPage, error) {
	if c.Workspace == "" {
		return nil, fmt.Errorf("workspace is required for wiki commands")
	}
	path := fmt.Sprintf("/workspaces/%s/wiki/pages/", c.Workspace)
	q := url.Values{}
	if archived {
		q.Set("archived", "true")
	}
	if updatedAfter != "" {
		q.Set("updated_after", updatedAfter)
	}
	body, err := c.GetRaw(path, q)
	if err != nil {
		return nil, err
	}
	return unmarshalListResponse[WikiPage](body)
}

func (c *Client) GetWikiPage(pageID string) (WikiPage, error) {
	if c.Workspace == "" {
		return nil, fmt.Errorf("workspace is required for wiki commands")
	}
	var page WikiPage
	path := fmt.Sprintf("/workspaces/%s/wiki/pages/%s/", c.Workspace, pageID)
	if err := c.Get(path, nil, &page); err != nil {
		return nil, err
	}
	return page, nil
}

func (c *Client) CreateWikiPage(payload map[string]interface{}) (WikiPage, error) {
	if c.Workspace == "" {
		return nil, fmt.Errorf("workspace is required for wiki commands")
	}
	var page WikiPage
	path := fmt.Sprintf("/workspaces/%s/wiki/pages/", c.Workspace)
	if err := c.Post(path, payload, &page); err != nil {
		return nil, err
	}
	return page, nil
}

func (c *Client) UpdateWikiPage(pageID string, payload map[string]interface{}) (WikiPage, error) {
	if c.Workspace == "" {
		return nil, fmt.Errorf("workspace is required for wiki commands")
	}
	var page WikiPage
	path := fmt.Sprintf("/workspaces/%s/wiki/pages/%s/", c.Workspace, pageID)
	if err := c.Patch(path, payload, &page); err != nil {
		return nil, err
	}
	return page, nil
}

func (c *Client) WikiPageAction(pageID, action string) (WikiPage, error) {
	if c.Workspace == "" {
		return nil, fmt.Errorf("workspace is required for wiki commands")
	}
	switch action {
	case "archive", "unarchive", "lock", "unlock":
	default:
		return nil, fmt.Errorf("unsupported wiki action %q", action)
	}
	var page WikiPage
	path := fmt.Sprintf("/workspaces/%s/wiki/pages/%s/%s/", c.Workspace, pageID, action)
	if err := c.Post(path, map[string]interface{}{}, &page); err != nil {
		return nil, err
	}
	return page, nil
}
