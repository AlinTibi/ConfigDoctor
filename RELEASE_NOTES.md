# Config Doctor v1.1.0

- Adds a dedicated actual-versus-baseline workflow, including `.env` versus `.env.example` and normalized JSON, YAML, TOML and INI key comparisons.
- Separates missing baseline keys, present keys, extra actual keys and empty actual values. Baseline presence does not establish requiredness; extra keys are informational.
- Diagnoses undefined `$NAME` and `${NAME}` references and cycle findings in `.env` files, with source keys, referenced names and line numbers.
- Resolves simple local chains against final file definitions within bounded limits. Undefined, cyclic or oversized expressions are retained. Single quotes and escaped dollars stay literal; complex default/shell expressions are not evaluated.
- Includes baseline classification and reference findings in HTML, JSON and TXT reports. Previews and default exports mask all scalar values and omit full source paths. Secret classification propagates through references.
- Preserves the existing Windows icon and local/offline workflow. Microsoft Edge WebView2 Runtime remains a separate prerequisite; nothing downloads or installs it automatically.

Portable Windows 10/11 x64 release. Extract the entire package and verify its accompanying SHA-256 checksum before running ConfigDoctor.exe.

Validation limitation: a fully network-isolated environment was not tested (NOT SAFELY REPRODUCIBLE / BLOCKED BY POLICY). Normal configuration processing is local/offline by design.
