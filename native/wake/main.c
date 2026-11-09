// Local keyword helper. stdin: uint16 LE byte count + S16_LE PCM; zero resets.
// stdout: R once ready, then 0/1 for each packet. No audio is saved or networked.
#include <pocketsphinx.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static int keyword_found(ps_decoder_t *decoder) {
    // KWS can report several pronunciation matches together. Do not compare
    // the concatenated hypothesis to a single string and lose a valid wake.
    for (ps_seg_t *seg = ps_seg_iter(decoder); seg; seg = ps_seg_next(seg)) {
        const char *word = ps_seg_word(seg);
        if (strcmp(word,"hey orion")==0 ||
            strcmp(word,"hey orion(2)")==0 ||
            strcmp(word,"hey orion(3)")==0) {
            ps_seg_free(seg);
            return 1;
        }
    }
    return 0;
}

int main(int argc, char **argv) {
    if (argc != 3) { fprintf(stderr,"usage: dotty-wake model-directory threshold\n"); return 2; }
    char hmm[4096], dict[4096], keywords[4096], *end;
    if (snprintf(hmm,sizeof(hmm),"%s/en-us",argv[1]) >= (int)sizeof(hmm) ||
        snprintf(dict,sizeof(dict),"%s/wake.dict",argv[1]) >= (int)sizeof(dict) ||
        snprintf(keywords,sizeof(keywords),"%s/wake.kws",argv[1]) >= (int)sizeof(keywords)) return 2;
    double threshold = strtod(argv[2], &end);
    if (*end || !(threshold > 0 && threshold < 1)) return 2;
    ps_config_t *config = ps_config_init(NULL);
    ps_config_set_str(config,"hmm",hmm);
    ps_config_set_str(config,"dict",dict);
    ps_config_set_str(config,"lm",NULL);
    ps_config_set_str(config,"kws",keywords);
    ps_config_set_float(config,"samprate",16000);
    ps_config_set_float(config,"kws_threshold",threshold);
    ps_config_set_str(config,"loglevel","ERROR");
    ps_decoder_t *decoder = ps_init(config);
    ps_config_free(config);
    // Feed only speech regions to the recognizer. A single indefinite
    // utterance lets idle room noise dominate its live acoustic statistics.
    ps_endpointer_t *ep = ps_endpointer_init(0, 0, 0, 16000, 0.03);
    if (!decoder || !ep) return 1;
    const char *cmn = ps_get_cmn(decoder, 0);
    if (!cmn) return 1;
    char *initial_cmn = malloc(strlen(cmn) + 1);
    if (!initial_cmn) return 1;
    strcpy(initial_cmn, cmn);
    size_t frame_size = ps_endpointer_frame_size(ep), pending = 0;
    int16_t *frame = calloc(frame_size, sizeof(*frame));
    if (!frame) return 1;
    int active = 0;
    setbuf(stdout,NULL);
    putchar('R');
    unsigned char bytes[4096], header[2];
    int16_t samples[2048];
    while (fread(header,1,2,stdin) == 2) {
        size_t len = header[0] | ((size_t)header[1] << 8);
        if (len > sizeof(bytes) || len%2) return 2;
        if (len == 0) {
            if (active && ps_end_utt(decoder)<0) return 1;
            // Conversation audio is deliberately absent from this stream.
            // Restore the tested acoustic baseline when local waiting resumes.
            if (ps_set_cmn(decoder, initial_cmn)<0 || ps_start_stream(decoder)<0) return 1;
            active = 0;
            pending = 0;
            ps_endpointer_free(ep);
            ep = ps_endpointer_init(0, 0, 0, 16000, 0.03);
            if (!ep) return 1;
            putchar('0'); continue;
        }
        if (fread(bytes,1,len,stdin) != len) return 2;
        for (size_t i=0;i<len/2;i++) samples[i]=(int16_t)(bytes[i*2] | (bytes[i*2+1]<<8));
        int found = 0;
        for (size_t i = 0; i < len/2; i++) {
            frame[pending++] = samples[i];
            if (pending < frame_size) continue;
            pending = 0;
            const int16 *speech = ps_endpointer_process(ep, frame);
            if (!speech) continue;
            if (!active) {
                if (ps_start_utt(decoder)<0) return 1;
                active = 1;
            }
            if (ps_process_raw(decoder,speech,frame_size,0,0)<0) return 1;
            int ended = !ps_endpointer_in_speech(ep);
            if (ended) {
                if (ps_end_utt(decoder)<0) return 1;
                active = 0;
            }
            if (keyword_found(decoder)) {
                found = 1;
                if (active && ps_end_utt(decoder)<0) return 1;
                active = 0;
            }
        }
        putchar(found?'1':'0');
    }
    if (active) ps_end_utt(decoder);
    free(initial_cmn); free(frame); ps_endpointer_free(ep); ps_free(decoder);
    return ferror(stdin)?1:0;
}
