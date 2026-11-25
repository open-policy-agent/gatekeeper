const assert = require("node:assert/strict");
const {execFileSync, spawnSync} = require("node:child_process");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const test = require("node:test");
const {prepare, report} = require("./benchmark.js");

const baseSha = "a".repeat(40);
const headSha = "b".repeat(40);

function fixture(permission = "write") {
  const calls = {created: [], updated: [], pulls: [], permissions: []};
  const outputs = {};
  const failures = [];
  const pr = {
    state: "open",
    base: {sha: baseSha},
    head: {sha: headSha, repo: {full_name: "contributor/gatekeeper"}},
  };
  const github = {rest: {
    repos: {getCollaboratorPermissionLevel: async (request) => {
      calls.permissions.push(request);
      return {data: {permission}};
    }},
    pulls: {get: async (request) => {
      calls.pulls.push(request);
      return {data: pr};
    }},
    issues: {
      createComment: async (request) => {
        calls.created.push(request);
        return {data: {id: 123}};
      },
      updateComment: async (request) => calls.updated.push(request),
    },
  }};
  const context = {
    repo: {owner: "open-policy-agent", repo: "gatekeeper"},
    issue: {number: 42},
    payload: {comment: {user: {login: "maintainer"}}},
    sha: "c".repeat(40),
    serverUrl: "https://github.com",
    runId: 99,
  };
  const core = {
    setOutput: (name, value) => {outputs[name] = value;},
    setFailed: (message) => failures.push(message),
  };
  const env = {
    COMMENT_ID: "123",
    BASE_SHA: baseSha,
    HEAD_SHA: headSha,
    BENCHMARK_RESULT: "success",
    DOWNLOAD_RESULT: "success",
  };
  return {github, context, core, pr, calls, outputs, failures, env};
}

function temporaryDirectory(t) {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "gatekeeper-benchmark-"));
  t.after(() => fs.rmSync(directory, {recursive: true, force: true}));
  return directory;
}

test("resolves the requested PR's base and fork head, not the default branch", async () => {
  const f = fixture();
  await prepare(f);
  assert.deepEqual(f.outputs, {
    base_sha: baseSha,
    head_sha: headSha,
    head_repo: "contributor/gatekeeper",
    comment_id: 123,
  });
  assert.deepEqual(f.calls.permissions, [{
    ...f.context.repo, username: "maintainer",
  }]);
  assert.deepEqual(f.calls.pulls, [{
    ...f.context.repo, pull_number: 42,
  }]);
  assert.equal(f.calls.created[0].issue_number, 42);
  assert.match(f.calls.created[0].body, new RegExp(headSha));
});

test("rejects read-only commenters before resolving or scheduling a benchmark", async () => {
  const f = fixture("read");
  await assert.rejects(prepare(f), /write access/);
  assert.deepEqual(f.calls.pulls, []);
  assert.deepEqual(f.calls.created, []);
  assert.deepEqual(f.outputs, {});
});

test("rejects closed PRs and unavailable head repositories", async (t) => {
  for (const [name, change, error] of [
    ["closed PR", (f) => {f.pr.state = "closed";}, /open pull request/],
    ["deleted fork", (f) => {f.pr.head.repo = null;}, /head repository/],
  ]) {
    await t.test(name, async () => {
      const f = fixture();
      change(f);
      await assert.rejects(prepare(f), error);
      assert.deepEqual(f.calls.created, []);
      assert.deepEqual(f.outputs, {});
    });
  }
});

test("updates only the request comment and fences untrusted report text", async (t) => {
  const directory = temporaryDirectory(t);
  const reportPath = path.join(directory, "benchstat.txt");
  fs.writeFileSync(reportPath, "BenchmarkExample 10ns\n```\nreport text\n");
  const f = fixture();
  await report({...f, reportPath});
  assert.equal(f.calls.created.length, 0);
  assert.equal(f.calls.updated.length, 1);
  assert.equal(f.calls.updated[0].comment_id, 123);
  assert.match(f.calls.updated[0].body, new RegExp(`Base: \`${baseSha}\``));
  assert.match(f.calls.updated[0].body, new RegExp(`PR: \`${headSha}\``));
  assert.match(f.calls.updated[0].body, /````\nBenchmarkExample/);
  assert.match(f.calls.updated[0].body, /\n````$/);
});

test("reports failed and cancelled benchmarks without reading result files", async () => {
  for (const result of ["failure", "cancelled"]) {
    const f = fixture();
    f.env.BENCHMARK_RESULT = result;
    f.env.DOWNLOAD_RESULT = "skipped";
    await report({...f, reportPath: "/nonexistent/benchstat.txt"});
    assert.match(f.calls.updated[0].body, new RegExp(`comparison ${result}`));
    assert.match(f.calls.updated[0].body, /actions\/runs\/99/);
  }
});

test("surfaces artifact download failures in the request comment and job", async () => {
  const f = fixture();
  f.env.DOWNLOAD_RESULT = "failure";
  await report({...f, reportPath: "/nonexistent/benchstat.txt"});
  assert.equal(f.failures.length, 1);
  assert.match(f.calls.updated[0].body, /could not be downloaded/);
});

test("updates the request comment when a downloaded report cannot be read", async (t) => {
  const directory = temporaryDirectory(t);
  for (const [reason, reportPath, expectedError] of [
    ["missing file", path.join(directory, "benchstat.txt"), /ENOENT/],
    ["directory instead of a file", directory, /EISDIR/],
  ]) {
    await t.test(reason, async () => {
      const f = fixture();
      await report({...f, reportPath});
      assert.equal(f.failures.length, 1);
      assert.match(f.failures[0], expectedError);
      assert.equal(f.calls.updated.length, 1);
      const [comment] = f.calls.updated;
      assert.equal(comment.comment_id, 123);
      assert.match(comment.body, /could not be read/);
      assert.match(comment.body, /https:\/\/github\.com\/open-policy-agent\/gatekeeper\/actions\/runs\/99/);
    });
  }
});

test("does not report an empty comparison as successful", async (t) => {
  const directory = temporaryDirectory(t);
  const reportPath = path.join(directory, "benchstat.txt");
  fs.writeFileSync(reportPath, " \n\t");
  const f = fixture();
  await report({...f, reportPath});
  assert.equal(f.failures.length, 1);
  assert.match(f.calls.updated[0].body, /did not produce any results/);
});

test("prepares each checkout's envtest version before invoking its benchmark target", (t) => {
  const directory = temporaryDirectory(t);
  const bin = path.join(directory, "bin");
  fs.mkdirSync(bin);
  const make = path.join(bin, "make");
  fs.writeFileSync(make, `#!${process.execPath}
const fs = require("node:fs");
const args = process.argv.slice(2);
if (args[0] === "benchmark-test") {
  const output = args.find((arg) => arg.startsWith("BENCHMARK_FILE_NAME=")).split("=").slice(1).join("=");
  fs.writeFileSync(output, JSON.stringify({assets: process.env.KUBEBUILDER_ASSETS, cwd: process.cwd()}));
} else if (args[0] !== "envtest") {
  throw new Error("Unexpected make target");
}
`);
  fs.chmodSync(make, 0o755);
  for (const [variable, version] of [
    ["ENVTEST_KUBERNETES_VERSION", "1.31.2"],
    ["ENVTEST_KUBERNETES_VERSION", "1.33.0"],
    ["KUBERNETES_VERSION", "1.28.0"],
  ]) {
    const checkout = path.join(directory, version);
    fs.mkdirSync(path.join(checkout, ".tmp/bin"), {recursive: true});
    fs.writeFileSync(path.join(checkout, "Makefile"),
      `${variable} ?= ${version}\n${variable === "ENVTEST_KUBERNETES_VERSION" ? "KUBERNETES_VERSION ?= 1.37.0\n" : ""}`);
    const envtest = path.join(checkout, ".tmp/bin/setup-envtest");
    fs.writeFileSync(envtest, `#!${process.execPath}
console.log(require("node:path").join(process.cwd(), ".tmp/bin", process.argv[3]));
`);
    fs.chmodSync(envtest, 0o755);
    const output = path.join(directory, `${version}.json`);
    execFileSync("bash", [path.join(__dirname, "run-benchmark.sh"), checkout, output], {
      env: {...process.env, PATH: `${bin}${path.delimiter}${process.env.PATH}`},
    });
    assert.deepEqual(JSON.parse(fs.readFileSync(output, "utf8")), {
      assets: path.join(checkout, ".tmp/bin", version),
      cwd: checkout,
    });
  }
  const missingVersion = path.join(directory, "missing-version");
  fs.mkdirSync(missingVersion);
  fs.writeFileSync(path.join(missingVersion, "Makefile"), "");
  const failed = spawnSync("bash", [
    path.join(__dirname, "run-benchmark.sh"), missingVersion, path.join(directory, "missing.json"),
  ], {
    encoding: "utf8",
    env: {...process.env, PATH: `${bin}${path.delimiter}${process.env.PATH}`},
  });
  assert.equal(failed.status, 1);
  assert.match(failed.stderr, /No envtest Kubernetes version/);
});
