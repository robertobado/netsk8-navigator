# Contributing a translation

Thanks for wanting to help translate Netsk8 Navigator! This is one of the
easiest ways to contribute — no Go, no build tooling knowledge, just one
TypeScript file with a list of `'English text': 'translated text'` pairs.

This guide covers everything you need: how the system works, how to add a
new language or improve an existing one, how to test your change, and how
to open the PR.

## How translations work here

There's no external i18n library — just one file,
[`frontend/src/lib/i18n.ts`](frontend/src/lib/i18n.ts). **English is the
source language everywhere in the app**, both frontend and backend: every
label you see in the UI, including things like "Ready", "Namespace", or
"Not yet bound to a PersistentVolume.", is a literal English string in the
code. That string doubles as its own translation key.

A translation is just a dictionary that maps each of those English strings
to its translated version:

```ts
const pt: Dict = {
  Name: 'Nome',
  Age: 'Idade',
  'No pods to display.': 'Nenhum pod para exibir.',
  // ...
}
```

If a string isn't in the dictionary yet, the app just shows the English
text — nothing breaks, it's just untranslated. So a translation PR can
start small and grow over time; it doesn't need to be 100% complete.

Today there's one real translation, `pt` (Brazilian Portuguese, ~230
entries), and `en`, which is nearly empty because English *is* the
fallback — most of the file's ~230 keys, translated to Portuguese, live in
that `pt` dictionary, and it's the best reference for what a complete
translation looks like.

## Option A — add a new language

This is the highest-impact contribution: a whole new language nobody's
covered yet.

1. Fork the repo and open `frontend/src/lib/i18n.ts`.
2. Copy the entire `pt` object (from `const pt: Dict = {` down to its
   closing `}`) and paste it right below, as your new language. Use the
   [BCP 47](https://en.wikipedia.org/wiki/IETF_language_tag) code as the
   variable name and rename the `app.subtitle` section header comments if
   you'd like — the section comments (`// --- DataTable ---`, etc.) just
   mark which component each group of keys came from, to make the file
   easier to navigate.

   ```ts
   const es: Dict = {
     // --- app chrome (pre-existing semantic keys) ---
     'app.subtitle': 'Gestión de Clúster',
     'app.allNamespaces': 'todos los namespaces',
     // ... translate every value below, keep every key on the left as-is
   }
   ```

3. Translate every value (the right-hand side of each `:`). **Never
   change the keys** (the left-hand side) — they're the exact English
   strings the app looks up at runtime, so changing one breaks the lookup
   for that string everywhere it's used.
4. Register your dictionary in `DICTS` and add a toggle entry to
   `LANGUAGES`, near the bottom of the file:

   ```ts
   const DICTS: Record<string, Dict> = { 'pt-BR': pt, en, es }

   export const LANGUAGES: ReadonlyArray<{ code: string; label: string }> = [
     { code: 'pt-BR', label: 'PT' },
     { code: 'en', label: 'EN' },
     { code: 'es', label: 'ES' },
   ]
   ```

   `label` is the short text shown on the sidebar's language toggle — 2–3
   letters is plenty.

That's it — no other files need to change. The sidebar toggle, all
frontend components, and every backend-sourced label all read from the
same dictionary automatically.

## Option B — fix or extend an existing translation

Found a wrong or awkward translation in `pt`, or a string that's still
showing up in English? Smaller, equally welcome PRs:

- **Fix a translation:** find the key in `pt` and edit its value.
- **Add a missing one:** if you spot English text that should be
  translated but isn't in the dictionary yet, add the `'English text':
  'Translated text'` pair. Put it near other keys from the same component
  if you can tell where it's from (the section comments help).

## A few translation notes

- **Kubernetes resource kinds stay untranslated.** `Pod`, `Deployment`,
  `Namespace`, `ConfigMap`, and so on are proper nouns from the Kubernetes
  API — the existing `pt` dictionary never translates them, and yours
  shouldn't either. Only UI chrome, labels, and sentences get translated.
- **Keep placeholders exactly as they are.** Some keys contain a
  placeholder like `{d}`, `{kind}`, or `{name}` — e.g. `'View {kind}
  details'`. Keep the `{...}` token verbatim in your translation; you can
  move it to wherever your language's word order needs it (see
  `'{d} ago'` in `pt`, translated as `'há {d}'` — the placeholder moved,
  the braces didn't).
- **Don't translate code-like content** — file paths, flag names, YAML
  keys — even when they appear inside an otherwise-translatable sentence.

## Testing your translation

```bash
cd frontend
pnpm install
pnpm dev
```

Open `http://localhost:5173`, then use the language toggle at the bottom
of the sidebar to switch to your language and click around. Most UI
chrome (nav, buttons, empty states, the events feed) is visible without a
cluster connection. Resource tables and detail panels need a real (or
local, e.g. [kind](https://kind.sigs.k8s.io) / [minikube](https://minikube.sigs.k8s.io))
Kubernetes cluster to populate — if you don't have one handy, that's fine,
just mention it in the PR and a maintainer can check that part.

Before opening the PR, run the same checks CI runs:

```bash
cd frontend
pnpm exec tsc -b      # typecheck
pnpm exec oxlint src  # lint
pnpm build             # production build
```

## Opening the PR

- Branch name: `i18n/add-<language>` (e.g. `i18n/add-spanish`) or
  `i18n/fix-pt-<short-description>` for a fix.
- PR title: `i18n: add <Language> translation` or `i18n: fix <language>
  <what changed>`.
- Only `frontend/src/lib/i18n.ts` should need to change — no backend code,
  no other frontend files. If your PR touches anything else, double-check
  you're not on the wrong branch.
- CI runs the same typecheck/lint/build commands above automatically.

That's the whole process. If anything here is unclear, open an issue —
improving this guide is a contribution too.
