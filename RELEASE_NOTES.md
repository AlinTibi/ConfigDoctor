# Config Doctor v1.1.0 — release candidate

- Adds a dedicated actual-versus-baseline workflow, including `.env` versus `.env.example` and normalized JSON, YAML, TOML and INI key comparisons.
- Separates missing baseline keys, present keys, extra actual keys and empty actual values. Baseline presence does not establish requiredness; extra keys are informational.
- Diagnoses undefined `$NAME` and `${NAME}` references and cycle findings in `.env` files, with source keys, referenced names and line numbers.
- Resolves simple local chains against final file definitions within bounded limits. Undefined, cyclic or oversized expressions are retained. Single quotes and escaped dollars stay literal; complex default/shell expressions are not evaluated.
- Includes baseline classification and reference findings in HTML, JSON and TXT reports. Previews and default exports mask all scalar values and omit full source paths. Secret classification propagates through references.
- Preserves the existing Windows icon and local/offline workflow. Microsoft Edge WebView2 Runtime remains a separate prerequisite; nothing downloads or installs it automatically.

The current public release remains v1.0.0. This candidate is prepared for review; no v1.1.0 tag or release is created by this change.
