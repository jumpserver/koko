#!/bin/bash
set -e
function init_jms_k8s_user(){
    echo `getent passwd | grep 'jms_k8s_user' || useradd -M -U -d /nonexistent jms_k8s_user` > /dev/null 2>&1
    echo `getent passwd | grep 'jms_k8s_user' | grep '/nonexistent'  || usermod -d /nonexistent jms_k8s_user` > /dev/null 2>&1
    echo `getent group | grep 'jms_k8s_user' || groupadd jms_k8s_user` > /dev/null 2>&1
}
export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
init_jms_k8s_user

if [ "${WELCOME_BANNER}" ]; then
    echo ${WELCOME_BANNER}
fi

mkdir -p /nonexistent
mount -t tmpfs -o size=10M tmpfs /nonexistent
cd /nonexistent
touch .bashrc
echo 'PS1="${K8S_NAME}# "' >> .bashrc
echo "export TERM=xterm" >> .bashrc
echo "source /usr/share/bash-completion/bash_completion" >> .bashrc
echo 'source /opt/kubectl-aliases/.kubectl_aliases' >> .bashrc
echo 'source <(kubectl completion bash)' >> .bashrc
echo 'complete -F __start_kubectl k' >> .bashrc
mkdir -p .kube

export HOME=/nonexistent
export LANG=en_US.UTF-8

echo `kubectl config set-credentials JumpServer-user --token=${KUBECTL_TOKEN}` > /dev/null 2>&1
echo `kubectl config set-cluster kubernetes --server=${KUBECTL_CLUSTER}` > /dev/null 2>&1
echo `kubectl config set-context kubernetes --namespace=${KUBECTL_NAMESPACE}` > /dev/null 2>&1
echo `kubectl config set-context kubernetes --cluster=kubernetes --user=JumpServer-user` > /dev/null 2>&1
echo `kubectl config use-context kubernetes` > /dev/null 2>&1

if [ ${KUBECTL_INSECURE_SKIP_TLS_VERIFY} == "true" ];then
    {
        clusters=`kubectl config get-clusters | tail -n +2`
        for s in ${clusters[@]}; do
            {
                echo `kubectl config set-cluster ${s} --insecure-skip-tls-verify=true` > /dev/null 2>&1
                echo `kubectl config unset clusters.${s}.certificate-authority-data` > /dev/null 2>&1
            } || {
                echo err > /dev/null 2>&1
            }
        done
    } || {
        echo err > /dev/null 2>&1
    }
fi

chown -R jms_k8s_user:jms_k8s_user .kube
chown -R jms_k8s_user:jms_k8s_user .bashrc

export TMPDIR=/nonexistent

exec su -s /bin/bash jms_k8s_user
