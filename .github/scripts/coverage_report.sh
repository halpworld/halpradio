#!/usr/bin/env bash
set -e

PR_COV=$(go tool cover -func=coverage.out | grep "total:" | grep -Eo "[0-9]+\.[0-9]+" || echo "0.0")
BASE_COV=""

if [ "${EVENT_NAME}" = "pull_request" ] && [ -n "${BASE_REF}" ]; then
  echo "Fetching base branch origin/${BASE_REF}..."
  git fetch origin "${BASE_REF}" --depth=1 || true
  git worktree add -f /tmp/base_tree "origin/${BASE_REF}" || true
  if [ -d "/tmp/base_tree" ]; then
    BASE_COV=$(cd /tmp/base_tree && go test -coverprofile=base_cov.out ./... > /dev/null 2>&1 && go tool cover -func=base_cov.out | grep "total:" | grep -Eo "[0-9]+\.[0-9]+" || echo "")
    git worktree remove --force /tmp/base_tree || true
  fi
fi

if [ -n "$PR_COV" ] && [ -n "$BASE_COV" ]; then
  DIFF=$(awk -v pr="$PR_COV" -v base="$BASE_COV" 'BEGIN { printf "%.1f", pr - base }')
  IS_INC=$(awk -v diff="$DIFF" 'BEGIN { print (diff > 0) ? 1 : 0 }')
  IS_DEC=$(awk -v diff="$DIFF" 'BEGIN { print (diff < 0) ? 1 : 0 }')

  if [ "$IS_INC" -eq 1 ]; then
    DELTA_DISPLAY="📈 **+${DIFF}%** (from \`${BASE_COV}%\` to \`${PR_COV}%\`)"
    MOTIVATION="🏆 **Awesome work!** Test coverage increased by **+${DIFF}%**! Your tests make \`halpradio\` rock solid for everyone. Keep up the great standards! 🚀"
  elif [ "$IS_DEC" -eq 1 ]; then
    DELTA_DISPLAY="📉 **${DIFF}%** (from \`${BASE_COV}%\` to \`${PR_COV}%\`)"
    MOTIVATION="💡 **Keep pushing!** Test coverage decreased by **${DIFF}%**. Adding unit tests for newly added functions will keep the codebase reliable and clean! 💪"
  else
    DELTA_DISPLAY="⚖️ **0.0%** (steady at \`${PR_COV}%\`)"
    MOTIVATION="✨ **Steady as she goes!** Test coverage remained constant at **${PR_COV}%**. Feel free to add edge-case tests if applicable! 🎯"
  fi
else
  DELTA_DISPLAY="\`${PR_COV}%\`"
  MOTIVATION="🎉 Test suite passed with **${PR_COV}%** total statement coverage!"
fi

DISPLAY_BASE="${BASE_REF:-main}"

cat <<EOF > /tmp/coverage_report.md
## 🧪 Test Coverage & Motivation Report

| Metric | Value |
| :--- | :--- |
| **Base Branch (\`${DISPLAY_BASE}\`)** | \`${BASE_COV:-N/A}%\` |
| **Pull Request Coverage** | \`${PR_COV}%\` |
| **Coverage Delta** | ${DELTA_DISPLAY} |

> ${MOTIVATION}

<details>
<summary>📋 Detailed Package Coverage Breakdown</summary>

\`\`\`
$(go tool cover -func=coverage.out)
\`\`\`

</details>
EOF

if [ -n "$GITHUB_STEP_SUMMARY" ]; then
  cat /tmp/coverage_report.md >> "$GITHUB_STEP_SUMMARY"
fi

if [ "${EVENT_NAME}" = "pull_request" ] && [ -n "${PR_NUMBER}" ]; then
  gh pr comment "${PR_NUMBER}" --body-file /tmp/coverage_report.md || true
fi
