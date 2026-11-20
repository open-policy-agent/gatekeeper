const fs = require("node:fs");

async function prepare({github, context, core}) {
  const {data: access} = await github.rest.repos.getCollaboratorPermissionLevel({
    ...context.repo,
    username: context.payload.comment.user.login,
  });
  if (!["admin", "maintain", "write"].includes(access.permission)) {
    throw new Error("Only repository collaborators with write access may run /benchmark.");
  }

  const {data: pr} = await github.rest.pulls.get({
    ...context.repo,
    pull_number: context.issue.number,
  });
  if (pr.state !== "open") {
    throw new Error("Benchmarks require an open pull request.");
  }
  if (!pr.head.repo) {
    throw new Error("The pull request's head repository is unavailable.");
  }

  const metadata = {
    base_sha: pr.base.sha,
    head_sha: pr.head.sha,
    head_repo: pr.head.repo.full_name,
  };
  const runUrl = `${context.serverUrl}/${context.repo.owner}/${context.repo.repo}/actions/runs/${context.runId}`;
  const {data: comment} = await github.rest.issues.createComment({
    ...context.repo,
    issue_number: context.issue.number,
    body: `Benchmark requested for base \`${metadata.base_sha}\` and PR \`${metadata.head_sha}\`.\n\n[Workflow run](${runUrl})`,
  });
  for (const [name, value] of Object.entries({...metadata, comment_id: comment.id})) {
    core.setOutput(name, value);
  }
}

async function report({github, context, core, env = process.env, reportPath = "results/benchstat.txt"}) {
  const commentId = Number(env.COMMENT_ID);
  if (!Number.isSafeInteger(commentId) || commentId <= 0) {
    throw new Error("The benchmark request comment ID is missing or invalid.");
  }
  if (!["success", "failure", "cancelled", "skipped"].includes(env.BENCHMARK_RESULT)) {
    throw new Error("The benchmark job result is missing or invalid.");
  }

  const runUrl = `${context.serverUrl}/${context.repo.owner}/${context.repo.repo}/actions/runs/${context.runId}`;
  let body = `Base: \`${env.BASE_SHA}\`\nPR: \`${env.HEAD_SHA}\`\n\n[Workflow run](${runUrl})\n\n`;
  if (env.BENCHMARK_RESULT === "success" && env.DOWNLOAD_RESULT === "success") {
    const results = fs.readFileSync(reportPath, "utf8");
    if (!results.trim()) {
      const message = "The benchmark comparison did not produce any results. See the workflow run for details.";
      core.setFailed(message);
      body += message;
    } else {
      let fenceLength = 3;
      for (const match of results.matchAll(/`+/g)) {
        fenceLength = Math.max(fenceLength, match[0].length + 1);
      }
      const fence = "`".repeat(fenceLength);
      body += `Benchmark comparison:\n\n${fence}\n${results}\n${fence}`;
    }
  } else if (env.BENCHMARK_RESULT === "success") {
    const message = "The benchmark results could not be downloaded. See the workflow run for details.";
    core.setFailed(message);
    body += message;
  } else {
    body += `Benchmark comparison ${env.BENCHMARK_RESULT}. See the workflow run for details.`;
  }
  await github.rest.issues.updateComment({
    ...context.repo,
    comment_id: commentId,
    body,
  });
}

module.exports = {prepare, report};
