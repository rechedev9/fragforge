package main

type skillInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"-"`
}

type skillDetail struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Body        string `json:"body"`
}

type skillIssue struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

type skillCheckResult struct {
	OK            bool         `json:"ok"`
	SkillsChecked int          `json:"skills_checked"`
	Issues        []skillIssue `json:"issues"`
}

type workflowDoc struct {
	Path              string
	Required          []string
	RequiredSkills    bool
	RequiredWorkflows bool
	Body              string
}

type workflowCheckResult struct {
	OK                         bool         `json:"ok"`
	SkillsChecked              int          `json:"skills_checked"`
	WorkflowsChecked           int          `json:"workflows_checked"`
	WorkflowDocsChecked        int          `json:"workflow_docs_checked"`
	AgentPromptWrappersChecked int          `json:"agent_prompt_wrappers_checked"`
	Issues                     []skillIssue `json:"issues"`
}

type workflowInfo struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Command     string   `json:"command"`
	RunCommand  string   `json:"run_command"`
	RunArgs     []string `json:"-"`
}
