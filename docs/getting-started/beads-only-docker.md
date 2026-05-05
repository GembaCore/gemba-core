# Beads-only Docker quickstart

Use these containers when you want to explore Gemba as a Beads viewer
and manager without setting up a project, GitHub, or agent
orchestration. The containers run the browser UI, the Go server, and the
Beads adaptor together.

## What you get

| Mode | Image | Best for | Writes |
| --- | --- | --- | --- |
| Quickstart sample project | `soflo1/gemba-core-quickstart:latest` | First look, demos, screenshots, learning the UI | Writable by default |
| Beads-only | `soflo1/gemba-core:latest` | Mounting your own Beads worktree or using a Dolt URL | Writable unless the source denies writes |
| Beads-read-only | Either image with `GEMBA_BEADS_READ_ONLY=true` | Review, audits, sharing a safe UI over real work | Blocked by Gemba before the adaptor |

The UI starts in Beads-only mode with Beads surfaces enabled: Flat and
Cascade board layouts, sort controls, milestone / epic / bead wrappers,
details in the right-hand panel, Beads health, Beads history, and the
Graph view. Agent sessions, dispatch, review, and escalation surfaces
are hidden because no orchestration plane is active.

## Authentication

The containers bind `0.0.0.0:7666` inside Docker, so token auth is on by
default.

On startup Gemba prints two credentials to the container log:

- A primary bearer token. It is printed only when the token is first
  created and is persisted as a hash in the `/data` volume.
- A one-time browser login URL on every server start:

```text
Open:   http://127.0.0.1:7666/#gemba-bootstrap=...
```

Use the one-time URL first. The browser exchanges the fragment token for
a session cookie and removes the fragment from the address bar. If the
link expires, open `http://127.0.0.1:7666` and paste the primary token
into the unlock prompt.

If you change the host port mapping, for example `-p 7777:7666`, edit
the printed URL to use the host port: `http://127.0.0.1:7777/...`.

## Quickstart sample project

This is the fastest way to see a populated board.

1. Pull the image:

   ```bash
   docker pull soflo1/gemba-core-quickstart:latest
   ```

2. Start the container:

   ```bash
   docker run --rm -it \
     --name gemba-beads-demo \
     -p 7666:7666 \
     -v gemba-quickstart-data:/data \
     soflo1/gemba-core-quickstart:latest
   ```

3. Copy the printed `Open:` URL from the terminal and open it in your
   browser.

4. Explore the UI:

   - Start on the Board in Flat layout.
   - Open a bead, epic, or milestone to inspect its right-hand details.
   - Switch to Cascade layout to see milestone → epic → bead structure.
   - Open Refine to groom the seeded Backlog items in a dense table.
   - Click **Graph** in the sidebar to inspect relationships and dependencies.
   - Open the Status tab to verify Beads health and current mode.
   - Create or edit a bead; the Beads history tab records the action.

The seeded project is stored in the `gemba-quickstart-data` Docker
volume. Reusing the same volume keeps your edits. To reset the demo:

```bash
docker volume rm gemba-quickstart-data
```

## Beads-only with your own local Beads worktree

Use the standard image when you already have a directory containing a
`.beads` database.

1. Pull the image:

   ```bash
   docker pull soflo1/gemba-core:latest
   ```

2. Start from the Beads worktree directory:

   ```bash
   cd /path/to/your/beads-worktree

   docker run --rm -it \
     --name gemba-beads \
     -p 7666:7666 \
     -v gemba-data:/data \
     -v "$PWD:/work" \
     -e GEMBA_BEADS_ONLY=true \
     -e GEMBA_BEADS_DIR=/work \
     soflo1/gemba-core:latest
   ```

3. Open the printed `Open:` URL.

This mode is writable by default. Creating, editing, deleting, and
moving beads writes to the mounted Beads database and appends entries to
the Beads history ledger.

## Beads-only with a Dolt URL

If your Beads database is served by Dolt/MySQL, point Gemba at the URL.
On Docker Desktop, `host.docker.internal` reaches a service running on
the host machine.

```bash
docker run --rm -it \
  --name gemba-beads-url \
  -p 7666:7666 \
  -v gemba-data:/data \
  -e GEMBA_BEADS_ONLY=true \
  -e GEMBA_BEADS_URL='mysql://root@host.docker.internal:3307/gemba' \
  soflo1/gemba-core:latest
```

URL mode is not automatically read-only. It is writable when the Dolt
server and credentials allow writes.

## Beads-read-only mode

Add `GEMBA_BEADS_READ_ONLY=true` when you want a safe inspection UI.
Gemba shows a **Beads-read-only** status pill, hides write affordances
where applicable, and rejects mutation requests before they reach Beads
or Dolt.

Read-only quickstart sample:

```bash
docker run --rm -it \
  --name gemba-beads-demo-ro \
  -p 7666:7666 \
  -v gemba-quickstart-data:/data \
  -e GEMBA_BEADS_READ_ONLY=true \
  soflo1/gemba-core-quickstart:latest
```

Read-only mounted worktree:

```bash
cd /path/to/your/beads-worktree

docker run --rm -it \
  --name gemba-beads-ro \
  -p 7666:7666 \
  -v gemba-data:/data \
  -v "$PWD:/work" \
  -e GEMBA_BEADS_ONLY=true \
  -e GEMBA_BEADS_READ_ONLY=true \
  -e GEMBA_BEADS_DIR=/work \
  soflo1/gemba-core:latest
```

Read-only Dolt URL:

```bash
docker run --rm -it \
  --name gemba-beads-url-ro \
  -p 7666:7666 \
  -v gemba-data:/data \
  -e GEMBA_BEADS_ONLY=true \
  -e GEMBA_BEADS_READ_ONLY=true \
  -e GEMBA_BEADS_URL='mysql://reader@host.docker.internal:3307/gemba' \
  soflo1/gemba-core:latest
```

For defense in depth, prefer read-only Dolt credentials for shared
review deployments. Gemba still enforces read-only mode even if the URL
credentials could write.

## Useful commands

Show the one-time URL again after starting in detached mode:

```bash
docker logs gemba-beads-demo
```

Run detached:

```bash
docker run -d \
  --name gemba-beads-demo \
  -p 7666:7666 \
  -v gemba-quickstart-data:/data \
  soflo1/gemba-core-quickstart:latest
docker logs gemba-beads-demo
```

Stop a detached container:

```bash
docker stop gemba-beads-demo
```

Use a different host port:

```bash
docker run --rm -it \
  -p 7777:7666 \
  -v gemba-quickstart-data:/data \
  soflo1/gemba-core-quickstart:latest
```

Then open `http://127.0.0.1:7777` or replace `:7666` with `:7777` in
the printed one-time URL.

## Troubleshooting

- **401 in the browser**: use the printed `Open:` URL or paste the
  primary token at the unlock prompt. If the container is detached, run
  `docker logs <container-name>`.
- **No sample data**: remove the demo volume and restart:
  `docker volume rm gemba-quickstart-data`.
- **Mounted worktree is empty**: make sure you started Docker from the
  directory that contains `.beads`, or set `GEMBA_BEADS_DIR` to the
  container path where that directory is mounted.
- **Dolt URL cannot connect**: from Docker Desktop use
  `host.docker.internal` for host services; on Linux use the host IP or
  run the Dolt server on a Docker network shared with Gemba.
- **Edits unexpectedly succeed in URL mode**: URL mode is writable by
  default. Set `GEMBA_BEADS_READ_ONLY=true` or use read-only Dolt
  credentials.
