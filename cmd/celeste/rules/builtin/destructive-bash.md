---
condition: (?i)\bgit\s+push\b[^\n;&|]*\s(--force|-f)(\s|$)|\brm\s+(-\S+\s+)*-([a-z]*r[a-z]*f|[a-z]*f[a-z]*r)[a-z]*(\s|$)|\brm\s+(-\S+\s+)*(--recursive\s+--force|--force\s+--recursive)(\s|$)
scope: tool_args:bash.command
action: interrupt
repeat: once
---
That bash command force-pushes or deletes recursively (git push --force, rm -rf). Do not run it unless the user asked for exactly that. Prefer a safer form (git push --force-with-lease, removing named paths), or ask the user first.
