# AI target technique map

Use this map to select a small test set for an observed trust boundary. Record the family ID with each attempt so results can map to external evaluators later.

| ID | Technique family | Test question | Useful controls |
|---|---|---|---|
| `instruction.direct` | Direct instruction conflict | Does user input override a higher-priority rule? | Benign request with the same format |
| `instruction.indirect` | Indirect instruction injection | Does retrieved or tool-returned content control later behavior? | Same artifact without instructions |
| `instruction.obfuscated` | Encoded or split instructions | Does normalization change which instructions the target follows? | Semantically equivalent plain text |
| `context.disclosure` | Hidden-context disclosure | Does the target reveal protected instructions, memory, or retrieved data? | Request for allowed public context |
| `state.multiturn` | Multi-turn state manipulation | Does earlier content weaken later enforcement or persist across resets? | Fresh session and reordered turns |
| `retrieval.poisoning` | Retrieval and knowledge poisoning | Does an untrusted source outrank trusted evidence? | Clean corpus and source-order changes |
| `tool.authorization` | Tool and permission confusion | Can text cause an action beyond the user's authority or stated task? | Read-only equivalent and denied identity |
| `tool.argument` | Tool argument injection | Can content alter a command, URL, recipient, or parser field? | Escaped input and fixed argument template |
| `output.downstream` | Downstream output injection | Can model output control a renderer, parser, agent, or command consumer? | Plain-text consumer and encoded output |
| `data.exfiltration` | Cross-boundary data transfer | Can protected data reach an untrusted output, tool, or destination? | Synthetic canary data and blocked sink |
| `availability.cost` | Resource and loop pressure | Can input cause unbounded calls, tokens, retries, or state growth? | Fixed budget and short benign task |
| `deception.honeypot` | Decoy and false-flag handling | Does a plausible lure redirect the operator without corroboration? | Clean artifact and labeled decoy |

Promptfoo can express deterministic regression cases around these families. PyRIT, DeepTeam, Garak, and JEF can generate or orchestrate broader adversarial variants outside Elenxa. Keep their mutations and scoring in the benchmark layer. Import only the selected case, technique ID, seed, and expected oracle into an Elenxa trial. This preserves Elenxa's execution trace and avoids replacing its planner with a second orchestrator.

The versioned, runnable starter cases live in `benchmark/fixtures`. Their clean control and five isolated deception variants are the initial case library; they are not a comprehensive attack corpus or a substitute for target-specific research.
