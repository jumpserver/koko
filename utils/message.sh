#!/usr/bin/env bash

BASE_DIR=$(cd $(dirname $0);pwd)
PROJECT_DIR=$(dirname ${BASE_DIR})

LANG="zh_CN zh_Hant en_US ja_JP"
DOMAIN=koko
BIN=${PROJECT_DIR}/cmd/i18ntool/geni18n.go
INPUT=pkg
OUTPUT=${PROJECT_DIR}/locale/

init_message() {
    for lang in $LANG;do
         output_dir=${OUTPUT}/${lang}/LC_MESSAGES/
         go run ${BIN} -domain ${DOMAIN} -in ${INPUT} -out ${output_dir}
    done
}

make_message() {
    cd ${PROJECT_DIR}
    go run ${BIN} -domain ${DOMAIN} -in ${INPUT} -out /tmp/
    for lang in $LANG;do
         po_file=${OUTPUT}/${lang}/LC_MESSAGES/${DOMAIN}.po
         msgmerge -U ${po_file} /tmp/${DOMAIN}.po
    done
    cd -
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


