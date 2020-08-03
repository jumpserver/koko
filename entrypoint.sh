#!/bin/sh
#

while [ "$(curl -I -m 10 -o /dev/null -s -w %{http_code} $CORE_HOST)" != "302" ]
do
    echo "wait for jms_core $CORE_HOST ready"
    sleep 2
done
echo "export TERM=xterm" >> /root/.bashrc
echo "source /usr/share/bash-completion/bash_completion" >> /root/.bashrc
echo 'source <(kubectl completion bash)' >> /root/.bashrc
echo 'complete -F __start_kubectl k' >> /root/.bashrc

cd /opt/koko
./koko
