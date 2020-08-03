#!/bin/bash
set -e

if [ "${WELCOME_BANNER}" ]; then
    echo ${WELCOME_BANNER}
fi

mkdir -p /nonexistent
mount -t tmpfs -o size=10M tmpfs /nonexistent
cd /nonexistent
cp /root/.bashrc ./
echo 'PS1="k8s > "' >> .bashrc
mkdir -p .kube

export HOME=/nonexistent

echo `rawkubectl config set-credentials JumpServer-user` > /dev/null 2>&1
echo `rawkubectl config set-cluster kubernetes --server=${KUBECTL_CLUSTER}` > /dev/null 2>&1
echo `rawkubectl config set-context kubernetes --cluster=kubernetes --user=JumpServer-user` > /dev/null 2>&1
echo `rawkubectl config use-context kubernetes` > /dev/null 2>&1

if [ ${KUBECTL_INSECURE_SKIP_TLS_VERIFY} == "true" ];then
    {
        clusters=`kubectl config get-clusters | tail -n +2`
        for s in ${clusters[@]}; do
            {
                echo `rawkubectl config set-cluster ${s} --insecure-skip-tls-verify=true` > /dev/null 2>&1
                echo `rawkubectl config unset clusters.${s}.certificate-authority-data` > /dev/null 2>&1
            } || {
                echo err > /dev/null 2>&1
            }
        done
    } || {
        echo err > /dev/null 2>&1
    }
fi

chown -R nobody:nogroup .kube

export TMPDIR=/nonexistent

exec su -s /bin/bash nobody