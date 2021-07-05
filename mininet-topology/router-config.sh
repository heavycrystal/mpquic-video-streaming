#!/bin/bash

sysctl net.ipv4.ip_forward=1
ifconfig router-eth0 10.0.0.2
ifconfig router-eth1 11.0.0.2
ifconfig router-eth2 100.0.0.2
