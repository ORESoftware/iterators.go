#!/usr/bin/env bash

# more stuff
eval $(ssh-agent)
ssh-add -D
ssh-add "$HOME/.ssh/id_vibe"
git fetch --all