#!/bin/sh
# Loads the seed inventory from the plan doc: 34 cables and 12 adapters.
# Usage: examples/seed-my-gear.sh   (set CBL=/path/to/cbl to use another binary)
set -e
cbl=${CBL:-cbl}

# USB
$cbl add usb-a usb-c --qty 4
$cbl add usb-a micro-usb --qty 2
$cbl add usb-a mini-usb --qty 6
$cbl add usb-a usb-b
$cbl add usb-a lightning
$cbl add usb-a-3.0 micro-usb
$cbl add usb-a-3.0 usb-b-3.0
$cbl add usbs lightning --notes 'labelled "USBS"'
$cbl add usbs usb-b --notes 'labelled "USBS"'

# Video
$cbl add hdmi hdmi --qty 2
$cbl add hdmi usb-c
$cbl add hdmi mini-dvi

# Analog AV and audio
$cbl add ps2-avmulti 'rca*3' --for PS2
$cbl add 'rca*3' 'rca*3'
$cbl add 'rca*2' 'rca*2'
$cbl add aux aux
$cbl add aux rca

# Power supplies
$cbl add outlet micro-usb --for 'Raspberry Pi'
$cbl add 3-prong barrel --variant 9v --qty 2 --name 'LG adaptor' --notes 'LG, rated 125 V 7 A; found, powers nothing yet'
$cbl add 2-prong barrel --variant unknown --qty 2
$cbl add 2-prong barrel --variant 8.5v --for PS2
$cbl add 2-prong barrel --variant 5v-2a --for PSP

# Adapters
$cbl adapter add 'us to eu plug adapter' --qty 3
$cbl adapter add 'usb-c wall charger'
$cbl adapter add 'usb-a wall charger'
$cbl adapter add '3-prong to 2-prong adapter'
$cbl adapter add 'lightning digital av adapter'
$cbl adapter add usb-c:m usb-a:f hdmi:f usb-c:f --name 'hub A'
$cbl adapter add 'usb-c hub with card reader' --name 'hub B'
$cbl adapter add sata esata --notes 'for the DVD drive'
$cbl adapter add 'multi-card to compactflash'
$cbl adapter add sata usb-a-3.0

# Devices. Models marked "ports unconfirmed" in `cbl device ls` rest on an
# assumption about the exact model; see `cbl device <name>`.
$cbl device add mac-mini-m2-2023 --name 'Mac mini'
$cbl device add macbook-pro-13-m1-2020 --name 'MacBook Pro'
$cbl device add thinkpad-t14-t15-gen1 --name 'ThinkPad'
$cbl device add raspberry-pi-3b --name 'Raspberry Pi'
$cbl device add iphone-5 --name 'iPhone 5'
$cbl device add ipod-2nd-gen --name 'iPod'
$cbl device add nintendo-3ds --name '3DS'
$cbl device add psp --name 'PSP'
$cbl device add ps2-slim --name 'PS2'
$cbl device add crt-tv --name 'CRT TV'
$cbl device add dell-monitor --name 'Dell monitor'
$cbl device add apple-tv-hd --name 'Apple TV'
$cbl device add camera-battery --name 'Camera (battery)'
$cbl device add camera-mini-usb --name 'Camera (mini-USB)'
$cbl device add camera-micro-usb --name 'Camera (micro-USB)'
$cbl device add te-ob-4 --name 'TE speaker'
