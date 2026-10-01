#!/bin/sh
# Regenerates the audio and video samples of the file viewers with ffmpeg, inside the tools container:
#   docker compose -f compose.dev.yaml run --rm tools sh testenv/preview-samples/generate-media.sh
set -eu
cd "$(dirname "$0")"
mkdir -p audio video
quiet="-hide_banner -loglevel error -y"

tone="sine=frequency=440:duration=4,volume=0.3"
ffmpeg $quiet -f lavfi -i "$tone" audio/tone.wav
ffmpeg $quiet -f lavfi -i "$tone" -c:a libmp3lame -b:a 64k -metadata title='Transferly tone' audio/tone.mp3
ffmpeg $quiet -f lavfi -i "$tone" -c:a libvorbis -q:a 2 audio/tone.ogg
ffmpeg $quiet -f lavfi -i "$tone" -c:a libopus -b:a 32k audio/tone.opus
ffmpeg $quiet -f lavfi -i "$tone" -c:a flac audio/tone.flac
ffmpeg $quiet -f lavfi -i "$tone" -c:a aac -b:a 64k audio/tone.m4a

picture="testsrc2=size=320x180:rate=24:duration=5"
sound="sine=frequency=660:duration=5,volume=0.2"
ffmpeg $quiet -f lavfi -i "$picture" -f lavfi -i "$sound" -c:v libx264 -pix_fmt yuv420p -preset veryfast -crf 30 -c:a aac -b:a 48k -movflags +faststart -shortest video/clip.mp4
ffmpeg $quiet -f lavfi -i "$picture" -f lavfi -i "$sound" -c:v libvpx-vp9 -b:v 200k -deadline realtime -c:a libopus -b:a 32k -shortest video/clip.webm
ffmpeg $quiet -f lavfi -i "$picture" -f lavfi -i "$sound" -c:v mpeg4 -q:v 8 -c:a mp3 -b:a 48k -shortest video/unplayable.avi
