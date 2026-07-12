# Native side of the Android shell: whisper.cpp + the verified bridge.
#
# Expects a whisper.cpp checkout at ./whisper.cpp (pinned to the version
# the bridge was verified against):
#   git clone --depth 1 --branch v1.6.2 \
#     https://github.com/ggerganov/whisper.cpp android/app/src/main/cpp/whisper.cpp
cmake_minimum_required(VERSION 3.22)
project(whisper_bridge C CXX)

set(WHISPER_BUILD_EXAMPLES OFF CACHE BOOL "" FORCE)
set(WHISPER_BUILD_TESTS OFF CACHE BOOL "" FORCE)
set(BUILD_SHARED_LIBS OFF CACHE BOOL "" FORCE)
add_subdirectory(whisper.cpp)

add_library(whisper_bridge SHARED
    whisper_bridge.c
    whisper_jni.c)

target_include_directories(whisper_bridge PRIVATE whisper.cpp)
target_link_libraries(whisper_bridge PRIVATE whisper log)
