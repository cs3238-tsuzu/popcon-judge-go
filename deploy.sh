#! /bin/bash

godep save
git add .
git commit -m "`date`"
git push origin transfer
ssh pcp@10.90.16.92 bash -c '/home/pcp/go/src/popcon-judge/install.sh'
