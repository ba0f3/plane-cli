package inject

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetModuleFlags() {
	includeAll = false
	includeProject = false
	includeModule = false
	includeState = false
	includeLabel = false
	includeType = false
	includeCycle = false
	includeWorkspace = false
	includeIntake = false
}

func TestGenerateContentIncludesAllFlagOptions(t *testing.T) {
	resetModuleFlags()
	t.Cleanup(resetModuleFlags)

	content := generateContent()

	assert.Contains(t, content, "`--all` - Include all command families")
	assert.Contains(t, content, "`--project` - Include project commands")
	assert.Contains(t, content, "`--module` - Include module commands")
	assert.Contains(t, content, "`--state` - Include state commands")
	assert.Contains(t, content, "`--label` - Include label commands")
	assert.Contains(t, content, "`--type` - Include type commands")
	assert.Contains(t, content, "`--workspace` - Include workspace commands")
	assert.Contains(t, content, "`--cycle` - Include cycle/sprint commands")
	assert.Contains(t, content, "`--intake` - Include intake commands")
}

func TestInjectCommandDescriptionReflectsCurrentBehavior(t *testing.T) {
	assert.Contains(t, InjectCmd.Long, "Issue command reference (plus optional modules via flags)")
	assert.Contains(t, InjectCmd.Long, "context and skills")
}

func TestGenerateContentUsesBashFenceForIssueCommands(t *testing.T) {
	resetModuleFlags()
	t.Cleanup(resetModuleFlags)

	content := generateContent()

	assert.Contains(t, content, "## Issue (aliases: i, issues, ticket)\n```bash")
}

func TestGenerateContentPointsToSnapshot(t *testing.T) {
	resetModuleFlags()
	t.Cleanup(resetModuleFlags)

	content := generateContent()

	assert.Contains(t, content, "### Reporting and bulk analysis")
	assert.Contains(t, content, "plane-cli snapshot --workspace <slug> --out ./plane-snapshot.json -o json")
	assert.Contains(t, content, "Do not run `issue list` followed by `issue view <id>` for every task")
}

func TestGenerateContentOrdersBulkAnalysisOverride(t *testing.T) {
	resetModuleFlags()
	t.Cleanup(resetModuleFlags)

	content := generateContent()

	issueIdx := strings.Index(content, "## Issue (aliases: i, issues, ticket)")
	bulkIdx := strings.Index(content, "### Reporting and bulk analysis")
	referenceIdx := strings.Index(content, "### Full Command Reference")

	require.NotEqual(t, -1, issueIdx, "issue command help must be present")
	require.NotEqual(t, -1, bulkIdx, "bulk analysis section must be present")
	require.NotEqual(t, -1, referenceIdx, "full command reference section must be present")
	assert.Less(t, issueIdx, bulkIdx, "bulk analysis section must follow the issue commands")
	assert.Less(t, bulkIdx, referenceIdx, "bulk analysis section must precede the full command reference")
}

func TestGenerateContentAllIncludesModernCommandFamilies(t *testing.T) {
	resetModuleFlags()
	t.Cleanup(resetModuleFlags)
	includeAll = true

	content := generateContent()

	assert.Contains(t, content, "plane-cli report summary")
	assert.Contains(t, content, "plane-cli digest user")
	assert.Contains(t, content, "plane-cli wiki list")
	assert.Contains(t, content, "plane-cli admin token")
	assert.Contains(t, content, "plane-cli raw <METHOD> <PATH>")
	assert.Contains(t, content, "plane-cli skills")
}
