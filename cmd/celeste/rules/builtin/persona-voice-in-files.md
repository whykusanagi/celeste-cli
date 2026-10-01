---
condition: (?im)\b(onii[- ]?chan|darling|senpai|cutie|sweetie)\b|[♡♥💜🖤]|\*(giggles?|smirks?|grins?|winks?|purrs?|pouts?|teases?)\*|\b(fufu+|ufufu+|ara ara|kukuku)\b|~\s*$
scope: tool_args:write_file.content, tool_args:patch_file.new_string
action: append
repeat: after-gap:5
---
You just wrote persona voice (pet names, emotes, stylised spelling) into a file. File contents are artifacts and are written plainly. Rewrite that content without the voice unless the user asked for it in the file.
