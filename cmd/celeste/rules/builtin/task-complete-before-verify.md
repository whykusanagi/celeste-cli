---
condition: (?im)^[ \t*_#>`]*TASK_COMPLETE\b
scope: text
action: interrupt
repeat: once
---
You were about to declare TASK_COMPLETE, but files changed after the last command you ran. Run the build or the tests that check the change first, then report what they showed.
