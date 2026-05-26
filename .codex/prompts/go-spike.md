# Go spike playbook for ZackVideo

Follow `AGENTS.md`.

Use this when the solution is uncertain and a small experiment is cheaper than a
full implementation. Keep the spike reversible.

Process:

1. Run `git status --short`.
2. State the hypothesis being tested.
3. Prefer throwaway files under `/tmp` or narrow local edits.
4. Do not change public APIs, DB schema, dependencies, generated media, or long
   render workflows unless explicitly requested.
5. Run the smallest command that validates or disproves the hypothesis.
6. If you edited repo files during the spike, either revert the spike edits or
   clearly list every remaining diff and why it should stay.
7. Summarize the result and recommend whether to proceed with real TDD work.

Output:

- Hypothesis
- Experiment
- Result
- Recommendation
- Remaining diffs, if any
