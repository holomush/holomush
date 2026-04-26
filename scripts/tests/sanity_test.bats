#!/usr/bin/env bats

@test "bats runner sees this file" {
  result=$((1 + 1))
  [ "$result" -eq 2 ]
}
