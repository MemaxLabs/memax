# Fonts

All four families are licensed under the SIL Open Font License 1.1, which allows them to be self-hosted and bundled with the app. Keep this note, or the OFL text from each family's source, next to the files when you ship them.

| File                                                            | Family                              | Use                                                                                |
| --------------------------------------------------------------- | ----------------------------------- | ---------------------------------------------------------------------------------- |
| `Gloock-Regular.woff2`                                          | Gloock 400                          | The name "Memax" only (logo lockup, cover)                                         |
| `Newsreader-Variable.woff2`, `Newsreader-Italic-Variable.woff2` | Newsreader, opsz 6–72, wght 200–800 | What is remembered: memories, Briefs, answers, page titles. Italic means proposed. |
| `SchibstedGrotesk-Variable.woff2`                               | Schibsted Grotesk, wght 400–900     | The interface: navigation, controls, tables, labels, meta                          |
| `IBMPlexMono-Regular.woff2`, `IBMPlexMono-Medium.woff2`         | IBM Plex Mono 400, 500              | Receipts only: receipt lines, IDs, paths, commands, the terminal                   |

`../tokens.css` declares the `@font-face` rules with `url('fonts/…')`, so keep this folder next to `tokens.css`.
