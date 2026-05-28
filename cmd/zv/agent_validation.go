package main

func checkAgentPromptWrappers() (int, []skillIssue, error) {
	codexChecked, codexIssues, err := checkCodexPromptWrappers()
	if err != nil {
		return 0, nil, err
	}
	claudeChecked, claudeIssues, err := checkClaudePromptWrappers()
	if err != nil {
		return 0, nil, err
	}
	issues := append(codexIssues, claudeIssues...)
	return codexChecked + claudeChecked, issues, nil
}
