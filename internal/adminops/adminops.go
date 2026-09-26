package adminops

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ba0f3/plane-cli/pkg/plane"
)

type Membership struct {
	Workspace string `json:"workspace"`
	Role      int    `json:"role,omitempty"`
}

type MemberInventory struct {
	ID             string       `json:"id,omitempty"`
	Email          string       `json:"email,omitempty"`
	Name           string       `json:"name,omitempty"`
	WorkspaceCount int          `json:"workspace_count"`
	Memberships    []Membership `json:"memberships"`
}

type ProjectSnapshot struct {
	Project plane.Project
	Members []plane.User
	Error   string
}

type WorkspaceSnapshot struct {
	Workspace string
	Members   []plane.User
	Projects  []ProjectSnapshot
	Error     string
}

type Finding struct {
	Severity  string `json:"severity"`
	Code      string `json:"code"`
	Workspace string `json:"workspace,omitempty"`
	Project   string `json:"project,omitempty"`
	Subject   string `json:"subject,omitempty"`
	Message   string `json:"message"`
}

type AuditSummary struct {
	Workspaces int `json:"workspaces"`
	Projects   int `json:"projects"`
	Members    int `json:"members"`
	Errors     int `json:"errors"`
	Warnings   int `json:"warnings"`
	Info       int `json:"info"`
}

type AuditResult struct {
	Summary  AuditSummary `json:"summary"`
	Findings []Finding    `json:"findings"`
}

type DeletePlan struct {
	Projects []plane.Project `json:"projects"`
	Missing  []string        `json:"missing,omitempty"`
}

func BuildMemberInventory(workspaces map[string][]plane.User, query string) []MemberInventory {
	type entry struct {
		ID          string
		Email       string
		Name        string
		Memberships []Membership
	}

	entries := map[string]*entry{}
	for workspace, members := range workspaces {
		for _, member := range members {
			key := memberKey(member)
			if key == "" {
				key = fmt.Sprintf("anonymous:%s:%d", workspace, len(entries))
			}
			current := entries[key]
			if current == nil {
				current = &entry{ID: member.ID, Email: member.Email, Name: userName(member)}
				entries[key] = current
			}
			if current.ID == "" {
				current.ID = member.ID
			}
			if current.Email == "" {
				current.Email = member.Email
			}
			if current.Name == "" {
				current.Name = userName(member)
			}
			current.Memberships = append(current.Memberships, Membership{Workspace: workspace, Role: member.Role})
		}
	}

	query = strings.ToLower(strings.TrimSpace(query))
	out := make([]MemberInventory, 0, len(entries))
	for _, current := range entries {
		if query != "" && !strings.Contains(strings.ToLower(current.ID+" "+current.Email+" "+current.Name), query) {
			continue
		}
		sort.Slice(current.Memberships, func(i, j int) bool {
			return current.Memberships[i].Workspace < current.Memberships[j].Workspace
		})
		out = append(out, MemberInventory{
			ID:             current.ID,
			Email:          current.Email,
			Name:           current.Name,
			WorkspaceCount: len(current.Memberships),
			Memberships:    current.Memberships,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		left := strings.ToLower(out[i].Email + "\x00" + out[i].Name + "\x00" + out[i].ID)
		right := strings.ToLower(out[j].Email + "\x00" + out[j].Name + "\x00" + out[j].ID)
		return left < right
	})
	return out
}

func Audit(snapshots []WorkspaceSnapshot) AuditResult {
	result := AuditResult{Findings: []Finding{}}
	uniqueMembers := map[string]struct{}{}

	for _, workspace := range snapshots {
		result.Summary.Workspaces++
		if workspace.Error != "" {
			appendFinding(&result, Finding{Severity: "error", Code: "workspace_unreadable", Workspace: workspace.Workspace, Message: workspace.Error})
			continue
		}
		workspaceMembers := map[string]plane.User{}
		for _, member := range workspace.Members {
			key := memberKey(member)
			if key != "" {
				workspaceMembers[key] = member
				uniqueMembers[key] = struct{}{}
			}
			if member.ID == "" && member.Email == "" {
				appendFinding(&result, Finding{
					Severity: "warning",
					Code: "member_missing_identity",
					Workspace: workspace.Workspace,
					Subject: userName(member),
					Message: "workspace member has neither id nor email",
				})
			}
		}
		if len(workspace.Members) == 0 {
			appendFinding(&result, Finding{Severity: "warning", Code: "workspace_without_members", Workspace: workspace.Workspace, Message: "workspace has no visible members"})
		}

		for _, project := range workspace.Projects {
			result.Summary.Projects++
			projectName := project.Project.Identifier
			if projectName == "" {
				projectName = project.Project.Name
			}
			if project.Error != "" {
				appendFinding(&result, Finding{Severity: "error", Code: "project_members_unreadable", Workspace: workspace.Workspace, Project: projectName, Message: project.Error})
				continue
			}
			if len(project.Members) == 0 {
				appendFinding(&result, Finding{Severity: "info", Code: "project_without_explicit_members", Workspace: workspace.Workspace, Project: projectName, Message: "project has no explicit members visible to this token"})
			}
			for _, member := range project.Members {
				key := memberKey(member)
				if key == "" {
					appendFinding(&result, Finding{Severity: "warning", Code: "project_member_missing_identity", Workspace: workspace.Workspace, Project: projectName, Subject: userName(member), Message: "project member has neither id nor email"})
					continue
				}
				if _, ok := workspaceMembers[key]; !ok {
					appendFinding(&result, Finding{
						Severity: "warning",
						Code: "project_member_not_in_workspace",
						Workspace: workspace.Workspace,
						Project: projectName,
						Subject: identityLabel(member),
						Message: "project member is not present in the visible workspace member list",
					})
				}
			}
		}
	}

	result.Summary.Members = len(uniqueMembers)
	sort.Slice(result.Findings, func(i, j int) bool {
		left := severityRank(result.Findings[i].Severity)
		right := severityRank(result.Findings[j].Severity)
		if left != right {
			return left > right
		}
		li := result.Findings[i].Workspace + "\x00" + result.Findings[i].Project + "\x00" + result.Findings[i].Code + "\x00" + result.Findings[i].Subject
		lj := result.Findings[j].Workspace + "\x00" + result.Findings[j].Project + "\x00" + result.Findings[j].Code + "\x00" + result.Findings[j].Subject
		return li < lj
	})
	return result
}

func ResolveProjects(projects []plane.Project, selectors []string) DeletePlan {
	plan := DeletePlan{Projects: []plane.Project{}, Missing: []string{}}
	seen := map[string]struct{}{}
	for _, selector := range selectors {
		selector = strings.TrimSpace(selector)
		matched := false
		for _, project := range projects {
			if !projectMatches(project, selector) {
				continue
			}
			matched = true
			if _, ok := seen[project.ID]; ok {
				break
			}
			seen[project.ID] = struct{}{}
			plan.Projects = append(plan.Projects, project)
			break
		}
		if !matched {
			plan.Missing = append(plan.Missing, selector)
		}
	}
	sort.Slice(plan.Projects, func(i, j int) bool {
		return strings.ToLower(plan.Projects[i].Identifier+plan.Projects[i].Name) < strings.ToLower(plan.Projects[j].Identifier+plan.Projects[j].Name)
	})
	return plan
}

func projectMatches(project plane.Project, selector string) bool {
	return strings.EqualFold(project.ID, selector) || strings.EqualFold(project.Identifier, selector) || strings.EqualFold(project.Name, selector)
}

func appendFinding(result *AuditResult, finding Finding) {
	result.Findings = append(result.Findings, finding)
	switch finding.Severity {
	case "error":
		result.Summary.Errors++
	case "warning":
		result.Summary.Warnings++
	default:
		result.Summary.Info++
	}
}

func memberKey(user plane.User) string {
	if user.ID != "" {
		return "id:" + strings.ToLower(user.ID)
	}
	if user.Email != "" {
		return "email:" + strings.ToLower(user.Email)
	}
	return ""
}

func userName(user plane.User) string {
	if user.DisplayName != "" {
		return user.DisplayName
	}
	return strings.TrimSpace(user.FirstName + " " + user.LastName)
}

func identityLabel(user plane.User) string {
	if user.Email != "" {
		return user.Email
	}
	if user.ID != "" {
		return user.ID
	}
	return userName(user)
}

func severityRank(severity string) int {
	switch severity {
	case "error":
		return 3
	case "warning":
		return 2
	default:
		return 1
	}
}
