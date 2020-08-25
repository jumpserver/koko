#!/bin/bash
set -e
function init_nobody_user(){
    echo `getent passwd | grep 'nobody' | grep '/nonexistent'  || usermod -d /nonexistent nobody` > /dev/null 2>&1
    echo `getent group | grep 'nogroup' || groupadd nogroup` > /dev/null 2>&1
}
export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
init_nobody_user

if [ "${WELCOME_BANNER}" ]; then
    echo ${WELCOME_BANNER}
fi

mkdir -p /nonexistent
mount -t tmpfs -o size=10M tmpfs /nonexistent
cd /nonexistent
touch .bashrc
echo 'PS1="# "' >> .bashrc
echo "export TERM=xterm" >> .bashrc
echo "source /usr/share/bash-completion/bash_completion" >> .bashrc
echo 'source /opt/kubectl-aliases/.kubectl_aliases' >> .bashrc
echo 'source <(kubectl completion bash)' >> .bashrc
echo 'complete -F __start_kubectl k' >> .bashrc
mkdir -p .kube

export HOME=/nonexistent

echo `rawkubectl config set-credentials JumpServer-user` > /dev/null 2>&1
echo `rawkubectl config set-cluster kubernetes --server=${KUBECTL_CLUSTER}` > /dev/null 2>&1
echo `rawkubectl config set-context kubernetes --cluster=kubernetes --user=JumpServer-user` > /dev/null 2>&1
echo `rawkubectl config use-context kubernetes` > /dev/null 2>&1

if [ ${KUBECTL_INSECURE_SKIP_TLS_VERIFY} == "true" ];then
    {
        clusters=`rawkubectl config get-clusters | tail -n +2`
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
chown -R nobody:nogroup .bashrc

export TMPDIR=/nonexistent

exec su -s /bin/bash nobody