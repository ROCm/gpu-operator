#!/bin/bash

A=$(git status $1 --porcelain --ignore-submodules=all | egrep -v '^[MADRC] ' | grep -v gitmodules | egrep -v '^DD ' | egrep -v "manifest" | egrep -v ".container_ready" | awk '{print $2}')
if [ -n "$A" ] ; then
    printf "=================================================================\n"
    printf "Failing compilation due to presence of locally modified files/conflicting changes for files : %s \n"  $A
    echo
    printf "Local changes i.e git-diff output of the tree follows. You can typically ignore changes to gitmodules and look into rest of diff..: \n"
    echo
    # This is a temporary hack till we fix the manifest generation to not rely on checked in files. This will unblock root and nic build targets
    git --no-pager diff $1 --ignore-submodules=all
    echo
    printf "**** This typically means the tree needs to be rebased to top of git-repo and any locally generated files need to be commited and PR needs to be resubmitted ****\n"
    printf "=================================================================\n"
	exit 1
fi
echo No uncommitted locally modified files
exit 0
