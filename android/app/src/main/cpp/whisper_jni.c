// Thin JNI glue over whisper_bridge.c — keep ALL whisper.cpp API usage in
// the bridge (which is compile- and run-verified on desktop, see
// android/README.md); this file only marshals Java types.
#include <jni.h>
#include <stdlib.h>

#include "whisper_bridge.h"

JNIEXPORT jlong JNICALL
Java_com_thesalleys_voiceinventory_WhisperLib_init(JNIEnv *env, jclass cls,
                                                   jstring model_path) {
    (void)cls;
    const char *path = (*env)->GetStringUTFChars(env, model_path, NULL);
    if (!path) {
        return 0;
    }
    int64_t h = vi_bridge_init(path);
    (*env)->ReleaseStringUTFChars(env, model_path, path);
    return (jlong)h;
}

JNIEXPORT void JNICALL
Java_com_thesalleys_voiceinventory_WhisperLib_free(JNIEnv *env, jclass cls,
                                                   jlong handle) {
    (void)env;
    (void)cls;
    vi_bridge_free((int64_t)handle);
}

JNIEXPORT jstring JNICALL
Java_com_thesalleys_voiceinventory_WhisperLib_transcribeWav(
    JNIEnv *env, jclass cls, jlong handle, jbyteArray wav, jstring lang,
    jint n_threads) {
    (void)cls;
    jsize len = (*env)->GetArrayLength(env, wav);
    jbyte *bytes = (*env)->GetByteArrayElements(env, wav, NULL);
    if (!bytes) {
        return NULL;
    }
    const char *lang_c = (*env)->GetStringUTFChars(env, lang, NULL);
    char *json = vi_bridge_transcribe_wav((int64_t)handle,
                                          (const uint8_t *)bytes, (size_t)len,
                                          lang_c ? lang_c : "auto",
                                          (int)n_threads);
    if (lang_c) {
        (*env)->ReleaseStringUTFChars(env, lang, lang_c);
    }
    (*env)->ReleaseByteArrayElements(env, wav, bytes, JNI_ABORT);
    if (!json) {
        return NULL;
    }
    jstring out = (*env)->NewStringUTF(env, json);
    vi_bridge_free_string(json);
    return out;
}
