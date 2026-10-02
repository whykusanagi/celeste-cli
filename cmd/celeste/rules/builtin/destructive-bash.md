---
condition: \S
scope: tool_args:bash.command
action: interrupt
repeat: after-gap:1
---
That bash command force-pushes or deletes recursively (git push --force, rm -rf). Do not run it unless the user asked for exactly that. Prefer a safer form (git push --force-with-lease, removing named paths; build output such as build/, dist/ or node_modules/ inside the project is fine), or ask the user first.
