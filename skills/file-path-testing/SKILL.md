---
name: file-path-testing
description: Use when testing uploads, downloads, archives, filenames, path traversal, content types, file parsing, or server-side file handling on an authorized target.
---

# File and path testing

Trace the file lifecycle from input through storage, parsing, retrieval, and execution context.

## Model the lifecycle

Record:

- client filename, server filename, storage location or URL, and ownership;
- extension, declared content type, detected content type, and parser selected;
- archive extraction, image or document transformation, and metadata handling;
- download authorization, response disposition, and browser rendering behavior;
- cleanup, replacement, versioning, and collision behavior.

## Test one boundary at a time

Use harmless marker files and reversible operations. Compare path normalization, alternate separators or encodings, duplicate names, nested archives, content-type disagreement, and retrieval under another authorized actor when the observed lifecycle makes them relevant.

Use `search_research_knowledge` before testing an unfamiliar file format, archive parser, or framework storage convention.

Confirm effects by retrieving the stored object, inspecting transformed output, or checking the task's oracle. A client-side acceptance message does not prove server storage, parsing, or execution.

## Completion

Report the lifecycle stage reached, original and effective names or paths, parser behavior, read-back evidence, and cleanup state. Distinguish traversal, unsafe rendering, parser abuse, authorization failure, and executable upload rather than grouping them as one upload issue.
