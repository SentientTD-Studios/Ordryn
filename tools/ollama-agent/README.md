# Local AI agent with Ollama

A small example agent that works GoTodo tasks with a model running on your own machine, so testing costs nothing.
It polls the agent's queue, sends each task to Ollama, then posts a comment, optionally updates the description,
moves the card, and finishes the run. Everything goes through the REST API with the agent's key, so the agent's
guardrails apply exactly as they would for Claude Code or any other client.

Requirements: Node 18+, [Ollama](https://ollama.com) running locally, and a GoTodo instance with the REST API
enabled (Admin → Settings) and Redis configured.

## The easy way: download it from the site

Most people don't need this folder. On the agent's settings page, **Connect the agent → Run a local model
(Ollama)** builds a single-file copy of this script with the model, context size, folder access, site URL and
(optionally) a fresh key built in. Run it with `node ordryn-agent-<handle>.mjs`. It works against any GoTodo
instance, hosted or local, because the script only makes outgoing HTTPS requests; no ports, tunnels, or server
config. Shell variables and a `.env` next to the file still override the built-in settings.

How it's built: `web/vite.config.ts` bundles `agent.mjs` and its modules with esbuild into the
`virtual:ollama-agent` module (loaded only when someone downloads). The generator
(`web/src/utils/ollamaAgentScript.ts`) replaces the `'__ORDRYN_AGENT_CONFIG__'` placeholder in `agent.mjs` with the
chosen settings as JSON. The build fails if that placeholder goes missing.

The rest of this page covers running from source.

## 1. Pick a model

```bash
ollama list                 # see what you have
ollama pull qwen3.5:9b      # if you need one
```

On start the script checks that Ollama is reachable and the model is installed, and prints the `ollama pull`
command if it isn't.

| Model | Use it for |
| --- | --- |
| `qwen3.5:9b` | Default. Fast, fits 8 GB GPUs. Good for testing the plumbing (triggers, guardrails, claims, run history) |
| `qwen3.8:27b` / `qwen3.6:35b` | Better answers once the plumbing works |
| `qwen3-coder:30b` / `qwen2.5-coder:14b` | Code-review or code-writing style tasks |

The script asks for JSON that matches a fixed schema (Ollama structured outputs), which small models follow more
reliably than multi-step tool calling. Thinking is off by default for speed; set `OLLAMA_THINK=true` to turn it on.

## 2. Set up the agent in GoTodo

1. Open a project → **Settings → AI agents → Add AI agent**. Pick **Triage & clarify** or **Draft & write**;
   a local chat model can't edit code, so skip **Code changes**. Example: name `Local Qwen`, handle `local_qwen`,
   role `Editor`. No GitHub repository is needed.
2. **Standing instructions**, for example:
   > Read the task and the discussion. Write a short plan or answer in your comment. If the description is vague,
   > rewrite it into clear steps. When done, move the card to Review. If you cannot help, ask a question and fail
   > the run.
3. **Triggers:** keep *@mention* on. On a board, add an "AI queue" column (Board tab) and check it under trigger
   columns. Under *Who can send work*, pick yourself (or your role).
4. **Guardrails:** check *Status* and *Description*. Under allowed columns, pick only *Review*, keep *complete tasks*
   off, and set *Max runs per hour* to something small like `10` while testing.
5. Leave **Webhook** empty. Webhooks need a public HTTPS URL; this agent polls instead.
6. Under **Connect the agent**, click **Create key** and copy it (shown once).

## 3. Run it

macOS / Linux:

```bash
export GOTODO_URL=http://localhost:8080
export GOTODO_AGENT_KEY=gotodo_...
export OLLAMA_MODEL=qwen3.5:9b
node tools/ollama-agent/agent.mjs
```

Windows PowerShell:

```powershell
$env:GOTODO_URL = "http://localhost:8080"
$env:GOTODO_AGENT_KEY = "gotodo_..."
$env:OLLAMA_MODEL = "qwen3.5:9b"
node tools/ollama-agent/agent.mjs
```

Then in GoTodo, comment `@local_qwen please break this down` on a task, drag a card into the AI queue column, or
use **Send to agent** on the task. Within `POLL_SECONDS` the agent claims the card, comments, moves it, and the run
shows up on the task and under **AI agents → Recent runs**.

Flags:

- `--once`: process the queue once and exit (handy for testing).
- `--dry-run`: print what the model would do without starting runs or changing anything.

| Variable | Default | |
| --- | --- | --- |
| `GOTODO_URL` | `http://localhost:8080` | Include `BASE_PATH` if you use one, e.g. `http://localhost:8080/gotodo` |
| `GOTODO_AGENT_KEY` | — | Required |
| `OLLAMA_URL` | `http://localhost:11434` | |
| `OLLAMA_MODEL` | `qwen3.5:9b` | |
| `OLLAMA_THINK` | `false` | `true` lets Qwen 3 models reason first (slower) |
| `POLL_SECONDS` | `10` | Minimum 2 |
| `REPO_DIR` | (off) | Folder the agent may **read** (see below) |
| `MAX_TOOL_STEPS` | `12` | Max file look-ups per run when `REPO_DIR` is set |
| `OLLAMA_NUM_CTX` | `32768` | Context window (tokens) requested from Ollama. Larger fits more code but needs more GPU memory; if `ollama ps` shows less than `100% GPU`, lower it |
| `REPO_WRITE` | `false` | `true` lets the agent edit files in `REPO_DIR` on its own git branch (see below) |
| `REPO_PROTECTED_BRANCHES` | `main,master` | Branches it will never edit; it must create a branch first |
| `MAX_FILE_WRITES` | `40` | Max file edits per run |

## Letting it read your code (read-only)

GoTodo never reads files on your machine. Its guardrails only cover what the agent may change inside GoTodo.
Code access is up to whatever runs the model. This agent can be given **read-only** access to one folder:

```powershell
$env:REPO_DIR = "M:\Projects\MyApp"         # your checkout
$env:OLLAMA_MODEL = "qwen3-coder:30b"       # a coding model works best here
node tools/ollama-agent/agent.mjs
```

With `REPO_DIR` set, the model gets three tools before it answers: `list_files`, `search_code` (regex or plain text), and
`read_file` (with line ranges). The console logs every look-up (`tool read_file {"path":…}`). Limits:

- Only that folder. Paths are resolved through symlinks, and anything outside it, including `..` tricks and links pointing
  out, is refused.
- Never readable: `.env*`, private keys and certificates (`*.pem`, `*.key`, `id_rsa`, …), `.npmrc`, credential and
  secrets files, plus the `.git`, `node_modules`, `dist`, `build`, and `vendor` folders.
- Nothing is written. The agent can't change files; it proposes changes as a ```` ```diff ```` block in its comment
  for a person to apply.
- Reads are capped (64 KB per read, 400 files per listing, `MAX_TOOL_STEPS` look-ups per run).

Pair it with the **Investigate code (read-only)** starting setup in GoTodo. Then comment something like
`@local_qwen why does login redirect to a 404?` and you get back the relevant files with line numbers, the likely cause,
and a proposed patch.

File contents go to the model you configure. With a local Ollama model they never leave your machine; if you point
`OLLAMA_URL` at a remote server, they go there.

Tests for the sandbox: `node --test tools/ollama-agent/repo.test.mjs`.

## Things to try

- Ask it to rewrite the title while *Title* is unchecked. The model is never offered a title field, so it says it can't
  instead of claiming a change. Check *Title* and ask again.
- Check *Tags* and ask it to tag the task. It sees the project's tags (it can't create new ones), and adding or removing
  one keeps the rest. Anything refused is listed under **Not applied** in its comment and in the run summary.
- Have someone not on the *Who can send work* list mention it. Nothing happens.
- Click **Pause** on the agent. The script logs `agent_paused` until you resume.
- Uncheck *Comment* in the guardrails and watch the refusal land in the run summary.

Which fields it can change: status, description, title, priority, and tags, each only when the guardrails allow it.
Due date, estimate, sprint, and custom fields aren't supported by this runner yet (use MCP for those).

## Using MCP instead

MCP clients that can drive a local model can connect to `http://localhost:8080/api/v2/mcp` with
`Authorization: Bearer <agent key>` and get the same tools Claude Code would (`list_my_runs`, `get_task`,
`add_comment`, `move_task`, `finish_run`, …). This script skips MCP so it has no dependencies.

## Letting it change your code (opt-in)

These limits belong to this example runner, not to GoTodo. Whoever runs the agent decides how much it may do on
their machine. To let it edit files, point `REPO_DIR` at the **top of a git checkout** and turn on `REPO_WRITE`:

```powershell
$env:REPO_DIR = "M:\Projects\MyApp"
$env:REPO_WRITE = "true"
$env:OLLAMA_MODEL = "qwen3-coder:30b"
node tools/ollama-agent/agent.mjs
```

The model gets three more tools: `create_branch`, `edit_file` (replace one exact snippet), and `write_file`
(create or replace a file). Ask for the work in the task or note, e.g. *"In a new branch, make greet() return
'Hello, name!' and mention it in the README."* What it can and can't do:

- **Branch first.** Nothing is written while `main`/`master` (or `REPO_PROTECTED_BRANCHES`) is checked out; the
  agent must call `create_branch`, which runs exactly `git checkout -b <name>` (validated name, no shell). If you
  ask for a new branch it creates one, named like `agent/task-246-short-summary`.
- **Any source file.** `.go`, `.vue`, `.js`, `.ts`, `.css`, Markdown, and so on. `edit_file` tolerates indentation
  differences (e.g. spaces in place of Go's tabs) and writes the file's own indentation back.
- **Same sandbox as reading.** Only inside `REPO_DIR`, no symlink escapes, never `.env`/keys/credentials, never
  `.git` or `node_modules`. At most `MAX_FILE_WRITES` edits per run, 256 KB per file.
- **No commands.** It can't commit, push, run tests, install packages, or run anything else. Changes are left
  uncommitted on the branch for you.
- **Honest report.** After the run, the agent appends the branch and the list of files it actually wrote (from its
  own write log, not the model's description) to its comment and run summary. Review with `git diff`, then commit
  or `git checkout main && git branch -D <branch>` to throw it away.
- **Your checkout moves.** `create_branch` switches the folder you point it at to the new branch. Use a separate
  clone or a `git worktree` for the agent if you keep working in your main checkout at the same time.
- `--dry-run` never writes.

Pair it with the **Code changes** starting setup in GoTodo, and keep **complete tasks** off so a person signs off.
Treat task text as instructions the agent will act on: limit who can trigger an editing agent.

Tests: `node --test tools/ollama-agent/repo-write.test.mjs`.

## Troubleshooting

**`Ollama 500: no user query found in messages`**: the conversation outgrew the model's context window. Ollama then
silently drops the oldest messages, including the task, and Qwen's template has no question left to answer. The agent
now keeps file contents to about half the window (oldest results are trimmed first), reads large files 300 lines at a
time, stops looking when the window is ~85% full, and answers with what it found instead of failing. If answers feel
under-informed on a big codebase, raise `OLLAMA_NUM_CTX` (e.g. `65536`) if your GPU has room. Each step logs
`~N of M context tokens used` so you can see how close it gets.

**Run fails with `agent_paused` or 401**: the agent was paused or removed in GoTodo, or the key was revoked.

**It reads lots of unrelated files**: put the area of the code in the task or the note (e.g. "comment rendering in
`web/src/components/TaskDiscussion.vue`"). Searching is cheap and reading is expensive, so specific tasks give faster,
better answers.
