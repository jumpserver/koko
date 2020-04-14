#!/usr/bin/env bash

generate_core_dump() {
  local koko_pid=$(ps -ef | grep 'koko' |grep -v 'unshare' | grep -v grep | awk '{print $2}')
  echo ${koko_pid}
  gcore -o kokoCore ${koko_pid}
  tar czvf data/debugkoko.tar.gz koko kokoCore.*
  rm kokoCore.*
}

generate_core_dump

