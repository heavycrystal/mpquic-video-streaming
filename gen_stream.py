import os
import sys
import subprocess
import hashlib

server_subfolder = "server-fs/"

resolution_tiers = [12, 6, 4, 2]
segment_time = 5

def sorter(a):  
    if a.count(".ts") == 0:
        return 0
    for i in range(len(a)):
        if a[i].isdigit():
            return (int(a[i]) * 100000) + int(''.join(char for char in a if char.isdigit()))


ffprobe_frame_rate = subprocess.Popen(["ffprobe", "-v", "error", "-select_streams", "v", "-of", "default=noprint_wrappers=1:nokey=1", "-show_entries", "stream=avg_frame_rate", server_subfolder + sys.argv[1]], stdin=None, stderr=None, stdout=subprocess.PIPE)
output, _ = ffprobe_frame_rate.communicate()
frame_rate = int(int(str(output, 'ascii').split('/')[0]) / int(str(output, 'ascii').split('/')[1][:-1]))

ffprobe_resolution = subprocess.Popen(["ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width,height", "-of", "csv=s=x:p=0", server_subfolder + sys.argv[1]], stdin=None, stderr=None, stdout=subprocess.PIPE)
output, _ = ffprobe_resolution.communicate()
width, height = int(str(output, 'ascii').split('x')[0]), int(str(output, 'ascii').split('x')[1][:-1])

if os.path.isdir(server_subfolder + sys.argv[1] + "-hls_stream/") == True:
    print("cached")
else:
    os.mkdir(server_subfolder + sys.argv[1] + "-hls_stream/")
    for i in range(len(resolution_tiers)):
        ffmpeg_call = subprocess.Popen(["ffmpeg", "-i", server_subfolder + sys.argv[1], "-hide_banner", "-loglevel", "error", "-c:v", "libx264", "-crf", "20", "-vf", "scale=" + str(2 * int(width / resolution_tiers[i])) + "x" + str(2 * int(height / resolution_tiers[i])), "-preset", "medium", "-g", str(frame_rate), "-sc_threshold", "0", "-c:a", "aac", "-b:a", "128k", "-ac", "2", "-f", "hls", "-hls_time", str(segment_time), "-hls_playlist_type", "event", server_subfolder + sys.argv[1] +"-hls_stream/"+ sys.argv[1] + "." + str(i) + ".m3u8"])
        ffmpeg_call.communicate()           


    lines = open(server_subfolder + sys.argv[1] + "-hls_stream/" + sys.argv[1] + ".0.m3u8").read().count("#") - 6
    metafest = open(server_subfolder + sys.argv[1] + "-hls_stream/" + sys.argv[1] + ".mfest", "w")
    metafest.write(str(lines) + "\n" + str(segment_time) + "\n" + str(len(resolution_tiers)) + "\n")   

    for i in range(len(resolution_tiers)):
        metafest.write(str(2 * int(width / resolution_tiers[i])) + "#" + str(2 * int(height / resolution_tiers[i])) + "\n")

    for i in sorted(os.listdir(server_subfolder + sys.argv[1] + "-hls_stream/"), key=sorter):
        if i.count(".ts") == 0:
            continue
        print(i)
        hasher = hashlib.sha256()
        hasher.update(open(server_subfolder + sys.argv[1] + "-hls_stream/" + i, "rb").read())
        metafest.write(hasher.hexdigest() + "\n")

    os.rename(server_subfolder + sys.argv[1] + "-hls_stream/" + sys.argv[1] + ".0.m3u8", server_subfolder + sys.argv[1] + "-hls_stream/" + sys.argv[1] + ".m3u8")

