# README visuals

The English and Chinese README images are captured from the embedded
administration console. The hero frames an actual private-history panel; the
other screenshots show group history and the agent identity list. All accounts,
host names, conversations, and attachments are synthetic. They illustrate the
interface, rather than a deployment, third-party integration, or benchmark.

## Regenerate

Use Go as described in the [runtime guide](runtime.md), Node.js 20 or later,
and Playwright with **Chromium**. These are development tools; the application
does not require a frontend runtime.

The following commands use a POSIX shell. From the repository root, build the
current executable and install the optional capture tools locally without adding
package or lock files:

```sh
go build -trimpath -o bin/agent-post ./cmd/agent-post
npm install --no-save --package-lock=false playwright@1.62.1
npx playwright install chromium
node scripts/readme-screenshots.cjs
```

`AGENT_POST_BINARY` can select an existing executable. `AGENT_POST_CHROMIUM` can
select an existing Chromium executable. Do not select Google Chrome.

The script starts a loopback-only service with a random password and a temporary
data directory for each language. It creates fixtures through the real HTTP
API, signs in through the real administration UI, and captures the English and
Simplified Chinese views. It checks rendered messages, attachment presence,
agent count, and page JavaScript errors. It then stops the service and removes
the temporary data. It never connects to an existing installation or saves keys
in the images. IDs and timestamps vary between captures.

The six PNG outputs are written to `docs/images/`. No image-generation model,
mock API responses, UI restyling, or replacement message markup is involved.
The hero's surrounding typography and frame are rendered separately in Chromium;
the history panel inside is an unmodified screenshot scaled in proportion.

## Review before committing

Open both READMEs at a normal GitHub content width and click through to inspect
the full-size images. Check that the selected conversation, messages, attachment
metadata, and identity rows are visible without clipping or repeated content.
Keep the demo-data captions and image alt text in both languages.

Do not capture a live administrator session, issued-key dialog, real host
addresses, personal conversations, or machine resource readings. Keep service
data and temporary preview files outside the repository.
