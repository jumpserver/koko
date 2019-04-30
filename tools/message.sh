#!/usr/bin/env bash

BASE_DIR=$(cd $(dirname $0);pwd)
PROJECT_DIR=$(dirname ${BASE_DIR})

LANG="zh_CN en_US"
DOMAIN=coco
BIN=${PROJECT_DIR}/cmd/geni18n.go
INPUT=pkg
OUTPUT=${PROJECT_DIR}/pkg/i18n/locale/

init_message() {
    for lang in $LANG;do
         output_dir=${OUTPUT}/${lang}/LC_MESSAGES/
         go run ${BIN} -domain ${DOMAIN} -in ${INPUT} -out ${output_dir}
    done
}

make_message() {
    go run ${BIN} -domain ${DOMAIN} -in ${INPUT} -out /tmp/
    for lang in $LANG;do
         po_file=${OUTPUT}/${lang}/LC_MESSAGES/${DOMAIN}.po
         msgmerge -U ${po_file} /tmp/${DOMAIN}.po
    done
}

compile_message() {
    for lang in $LANG;do
         po_file=${OUTPUT}/${lang}/LC_MESSAGES/${DOMAIN}.po
         msgfmt -o ${po_file/po/mo} ${po_file}
    done
}

if [[ $1 == "c" || $1 == "compile" ]];then
    compile_message
elif [[ $1 == "i" ]];then
    init_message
else
    make_message
fi


