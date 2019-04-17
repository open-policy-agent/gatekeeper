#!/usr/bin/env bash

########################
# include the magic
########################
. ../demo-magic.sh


########################
# Configure the options
########################

#
# speed at which to simulate typing. bigger num = faster
#
# TYPE_SPEED=20

#
# custom prompt
#
# see http://www.tldp.org/HOWTO/Bash-Prompt-HOWTO/bash-prompt-escape-sequences.html for escape sequences
#
DEMO_PROMPT="${GREEN}➜ ${CYAN}\W "

# hide the evidence
clear


# put your demo awesomeness here
if [ ! -d "stuff" ]; then
  pe "mkdir stuff"
fi

pe "cd stuff"

pe "ls"

p "cat \"something you dont want to really run\""

# show a prompt so as not to reveal our true nature after
# the demo has concluded
p ""
