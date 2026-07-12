#include "whisper_bridge.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "whisper.h"

// --- canonical WAV (audio.EncodeWAV16: 44-byte header, PCM16 mono 16 kHz)

#define VI_WAV_HEADER 44
#define VI_SAMPLE_RATE 16000

static float *vi_decode_wav(const uint8_t *wav, size_t len, int *n_out) {
    if (len <= VI_WAV_HEADER || memcmp(wav, "RIFF", 4) != 0 ||
        memcmp(wav + 8, "WAVE", 4) != 0 || memcmp(wav + 36, "data", 4) != 0) {
        return NULL;
    }
    uint32_t data_len = (uint32_t)wav[40] | ((uint32_t)wav[41] << 8) |
                        ((uint32_t)wav[42] << 16) | ((uint32_t)wav[43] << 24);
    if (data_len > len - VI_WAV_HEADER) {
        data_len = (uint32_t)(len - VI_WAV_HEADER);
    }
    int n = (int)(data_len / 2);
    float *pcm = (float *)malloc(sizeof(float) * (size_t)(n > 0 ? n : 1));
    if (!pcm) {
        return NULL;
    }
    const uint8_t *d = wav + VI_WAV_HEADER;
    for (int i = 0; i < n; i++) {
        int16_t v = (int16_t)((uint16_t)d[i * 2] | ((uint16_t)d[i * 2 + 1] << 8));
        pcm[i] = (float)v / 32768.0f;
    }
    *n_out = n;
    return pcm;
}

// --- growable output buffer with JSON string escaping

typedef struct {
    char *buf;
    size_t len;
    size_t cap;
    int oom;
} vi_sb;

static void vi_sb_grow(vi_sb *b, size_t need) {
    if (b->oom || b->len + need + 1 <= b->cap) {
        return;
    }
    size_t cap = b->cap ? b->cap : 4096;
    while (cap < b->len + need + 1) {
        cap *= 2;
    }
    char *nb = (char *)realloc(b->buf, cap);
    if (!nb) {
        b->oom = 1;
        return;
    }
    b->buf = nb;
    b->cap = cap;
}

static void vi_sb_raw(vi_sb *b, const char *s) {
    size_t n = strlen(s);
    vi_sb_grow(b, n);
    if (b->oom) {
        return;
    }
    memcpy(b->buf + b->len, s, n);
    b->len += n;
    b->buf[b->len] = '\0';
}

static void vi_sb_fmt(vi_sb *b, const char *fmt, long long v) {
    char tmp[32];
    snprintf(tmp, sizeof tmp, fmt, v);
    vi_sb_raw(b, tmp);
}

static void vi_sb_float(vi_sb *b, double v) {
    char tmp[48];
    snprintf(tmp, sizeof tmp, "%.6f", v);
    vi_sb_raw(b, tmp);
}

static void vi_sb_jstr(vi_sb *b, const char *s) {
    vi_sb_raw(b, "\"");
    for (const unsigned char *p = (const unsigned char *)s; *p; p++) {
        char tmp[8];
        switch (*p) {
        case '"':
            vi_sb_raw(b, "\\\"");
            break;
        case '\\':
            vi_sb_raw(b, "\\\\");
            break;
        case '\n':
            vi_sb_raw(b, "\\n");
            break;
        case '\r':
            vi_sb_raw(b, "\\r");
            break;
        case '\t':
            vi_sb_raw(b, "\\t");
            break;
        default:
            if (*p < 0x20) {
                snprintf(tmp, sizeof tmp, "\\u%04x", *p);
                vi_sb_raw(b, tmp);
            } else {
                tmp[0] = (char)*p;
                tmp[1] = '\0';
                vi_sb_raw(b, tmp);
            }
        }
    }
    vi_sb_raw(b, "\"");
}

// --- bridge API

int64_t vi_bridge_init(const char *model_path) {
    struct whisper_context_params cparams = whisper_context_default_params();
    struct whisper_context *ctx =
        whisper_init_from_file_with_params(model_path, cparams);
    return (int64_t)(intptr_t)ctx;
}

void vi_bridge_free(int64_t handle) {
    struct whisper_context *ctx = (struct whisper_context *)(intptr_t)handle;
    if (ctx) {
        whisper_free(ctx);
    }
}

char *vi_bridge_transcribe_wav(int64_t handle, const uint8_t *wav, size_t wav_len,
                               const char *lang, int n_threads) {
    struct whisper_context *ctx = (struct whisper_context *)(intptr_t)handle;
    if (!ctx || !wav) {
        return NULL;
    }
    int n_samples = 0;
    float *pcm = vi_decode_wav(wav, wav_len, &n_samples);
    if (!pcm || n_samples <= 0) {
        free(pcm);
        return NULL;
    }

    struct whisper_full_params params =
        whisper_full_default_params(WHISPER_SAMPLING_GREEDY);
    params.print_progress = false;
    params.print_realtime = false;
    params.print_special = false;
    params.print_timestamps = false;
    params.token_timestamps = true;
    params.translate = false;
    params.no_context = true;
    if (n_threads > 0) {
        params.n_threads = n_threads;
    }
    params.language = (lang && lang[0]) ? lang : "auto";

    int rc = whisper_full(ctx, params, pcm, n_samples);
    free(pcm);
    if (rc != 0) {
        return NULL;
    }

    vi_sb b = {0};
    vi_sb_raw(&b, "{\"result\":{\"language\":");
    vi_sb_jstr(&b, whisper_lang_str(whisper_full_lang_id(ctx)));
    vi_sb_raw(&b, "},\"transcription\":[");

    const int n_seg = whisper_full_n_segments(ctx);
    for (int i = 0; i < n_seg; i++) {
        if (i > 0) {
            vi_sb_raw(&b, ",");
        }
        vi_sb_raw(&b, "{\"text\":");
        vi_sb_jstr(&b, whisper_full_get_segment_text(ctx, i));
        vi_sb_raw(&b, ",\"offsets\":{\"from\":");
        vi_sb_fmt(&b, "%lld", (long long)whisper_full_get_segment_t0(ctx, i) * 10);
        vi_sb_raw(&b, ",\"to\":");
        vi_sb_fmt(&b, "%lld", (long long)whisper_full_get_segment_t1(ctx, i) * 10);
        vi_sb_raw(&b, "},\"tokens\":[");
        const int n_tok = whisper_full_n_tokens(ctx, i);
        for (int j = 0; j < n_tok; j++) {
            whisper_token_data td = whisper_full_get_token_data(ctx, i, j);
            if (j > 0) {
                vi_sb_raw(&b, ",");
            }
            vi_sb_raw(&b, "{\"text\":");
            vi_sb_jstr(&b, whisper_full_get_token_text(ctx, i, j));
            vi_sb_raw(&b, ",\"p\":");
            vi_sb_float(&b, td.p);
            vi_sb_raw(&b, ",\"offsets\":{\"from\":");
            vi_sb_fmt(&b, "%lld", (long long)td.t0 * 10);
            vi_sb_raw(&b, ",\"to\":");
            vi_sb_fmt(&b, "%lld", (long long)td.t1 * 10);
            vi_sb_raw(&b, "}}");
        }
        vi_sb_raw(&b, "]}");
    }
    vi_sb_raw(&b, "]}");

    if (b.oom) {
        free(b.buf);
        return NULL;
    }
    return b.buf;
}

void vi_bridge_free_string(char *s) { free(s); }
