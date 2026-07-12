// Desktop test driver for whisper_bridge.c: model + canonical WAV → JSON.
#include <stdio.h>
#include <stdlib.h>
#include "whisper_bridge.h"

int main(int argc, char **argv) {
    if (argc != 4) { fprintf(stderr, "usage: %s model wav lang\n", argv[0]); return 2; }
    FILE *f = fopen(argv[2], "rb");
    if (!f) { perror("wav"); return 1; }
    fseek(f, 0, SEEK_END); long len = ftell(f); fseek(f, 0, SEEK_SET);
    uint8_t *buf = malloc(len);
    if (fread(buf, 1, len, f) != (size_t)len) { fprintf(stderr, "short read\n"); return 1; }
    fclose(f);
    int64_t h = vi_bridge_init(argv[1]);
    if (!h) { fprintf(stderr, "init failed\n"); return 1; }
    char *json = vi_bridge_transcribe_wav(h, buf, len, argv[3], 8);
    if (!json) { fprintf(stderr, "transcribe failed\n"); return 1; }
    printf("%s\n", json);
    vi_bridge_free_string(json);
    vi_bridge_free(h);
    free(buf);
    return 0;
}
