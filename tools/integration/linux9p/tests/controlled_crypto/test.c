/* SPDX-License-Identifier: GPL-2.0-only */
/* Production strict verifier, real OpenSSL signing, no provider authority. */
#include "hfs_crypto.h"
#include <assert.h>
#include <openssl/evp.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

int hfs_ed25519_point_validate(const unsigned char bytes[32], int key);

static void unhex(const char *text, unsigned char *bytes, size_t length)
{
	unsigned int value;
	size_t i;

	assert(strlen(text) == length * 2);
	for (i = 0; i < length; i++) {
		assert(sscanf(text + i * 2, "%2x", &value) == 1);
		bytes[i] = value;
	}
}

static int openssl_verify(const unsigned char key[32], const void *message,
			 size_t length, const unsigned char signature[64])
{
	EVP_PKEY *public = EVP_PKEY_new_raw_public_key(EVP_PKEY_ED25519,
						    NULL, key, 32);
	EVP_MD_CTX *context = EVP_MD_CTX_new();
	int result;

	assert(public && context);
	assert(EVP_DigestVerifyInit(context, NULL, NULL, NULL, public) == 1);
	result = EVP_DigestVerify(context, signature, 64, message, length);
	EVP_MD_CTX_free(context);
	EVP_PKEY_free(public);
	return result == 1;
}

static void rfc8032(void)
{
	unsigned char key[32], signature[64];

	unhex("d75a980182b10ab7d54bfed3c964073a"
	      "0ee172f3daa62325af021a68f707511a", key, 32);
	unhex("e5564300c360ac729086e2cc806e828a"
	      "84877f1eb8e5d974d873e06522490155"
	      "5fb8821590a33bacc61e39701cf9b46b"
	      "d25bf5f0595bbe24655141438e7a100b", signature, 64);
	assert(!hfs_ed25519_key_validate(key));
	assert(!hfs_ed25519_verify(key, "", 0, signature));
	assert(openssl_verify(key, "", 0, signature));
	signature[63] |= 0x80;
	assert(hfs_ed25519_verify(key, "", 0, signature));
}

static void actual_signatures(void)
{
	static const size_t lengths[] = { 0, 1, 255, 16384, 131072 };
	static unsigned char message[131073];
	unsigned char seed[32], key[32], signature[64], changed[64];
	EVP_MD_CTX *context;
	EVP_PKEY *private;
	size_t round, i, key_length, signature_length, length;

	memset(message, 0x5a, sizeof(message));
	for (round = 0; round < sizeof(lengths) / sizeof(lengths[0]); round++) {
		memset(seed, round + 7, sizeof(seed));
		private = EVP_PKEY_new_raw_private_key(EVP_PKEY_ED25519,
						      NULL, seed, sizeof(seed));
		context = EVP_MD_CTX_new();
		assert(private && context);
		key_length = sizeof(key);
		assert(EVP_PKEY_get_raw_public_key(private, key, &key_length) == 1);
		assert(!hfs_ed25519_key_validate(key));
		length = lengths[round];
		assert(EVP_DigestSignInit(context, NULL, NULL, NULL, private) == 1);
		signature_length = sizeof(signature);
		assert(EVP_DigestSign(context, signature, &signature_length,
				      message, length) == 1);
		assert(!hfs_ed25519_verify(key, message, length, signature));
		assert(openssl_verify(key, message, length, signature));
		for (i = 0; i < sizeof(signature); i++) {
			memcpy(changed, signature, sizeof(changed));
			changed[i] ^= 1;
			assert(hfs_ed25519_verify(key, message, length, changed));
		}
		message[0] ^= 1;
		if (length)
			assert(hfs_ed25519_verify(key, message, length, signature));
		message[0] ^= 1;
		assert(hfs_ed25519_verify(key, message, 131073, signature));
		EVP_MD_CTX_free(context);
		EVP_PKEY_free(private);
	}
}

static void point_policy(void)
{
	unsigned char point[32], identity[32] = { 1 }, signature[64] = { 1 };
	unsigned char prime[32], public[32], mixed[32];
	unsigned int borrow = 0, value;
	size_t i;

	assert(hfs_ed25519_key_validate(identity));
	assert(!hfs_ed25519_point_validate(identity, 0));
	assert(hfs_ed25519_verify(identity, "arbitrary", 9, signature));
	printf("OpenSSL identity-key/identity-R/zero-S acceptance: %d\n",
	       openssl_verify(identity, "arbitrary", 9, signature));
	memset(prime, 0xff, 32);
	prime[0] = 0xed;
	prime[31] = 0x7f;
	assert(hfs_ed25519_point_validate(prime, 0));
	memcpy(point, identity, 32);
	point[31] = 0x80; /* Invalid sign bit for x=0. */
	assert(hfs_ed25519_point_validate(point, 0));
	memset(point, 0, 32); /* Order four. */
	assert(hfs_ed25519_point_validate(point, 0));
	memcpy(point, prime, 32);
	point[0]--; /* y=-1, order two. */
	assert(hfs_ed25519_point_validate(point, 0));
	unhex("d75a980182b10ab7d54bfed3c964073a"
	      "0ee172f3daa62325af021a68f707511a", public, 32);
	/* Adding the order-two point maps (x,y) to (-x,-y). Construct
	 * its canonical encoding with integer p-y; it is mixed order.
	 */
	for (i = 0; i < 32; i++) {
		value = (unsigned int)prime[i] -
			(public[i] & (i == 31 ? 0x7f : 0xff)) - borrow;
		mixed[i] = value;
		borrow = value > 255;
	}
	mixed[31] |= (public[31] ^ 0x80) & 0x80;
	assert(hfs_ed25519_point_validate(mixed, 0));
	assert(hfs_ed25519_key_validate(mixed));
}

static void verify_go(void)
{
	char *line = NULL;
	size_t capacity = 0, length;
	unsigned char key[32], signature[64], *message;

	assert(getline(&line, &capacity, stdin) > 0);
	line[strcspn(line, "\n")] = 0;
	unhex(line, key, 32);
	assert(getline(&line, &capacity, stdin) > 0);
	line[strcspn(line, "\n")] = 0;
	unhex(line, signature, 64);
	assert(getline(&line, &capacity, stdin) > 0);
	line[strcspn(line, "\n")] = 0;
	length = strlen(line) / 2;
	assert(length <= 131072);
	message = malloc(length ? length : 1);
	assert(message);
	unhex(line, message, length);
	assert(!hfs_ed25519_verify(key, message, length, signature));
	assert(openssl_verify(key, message, length, signature));
	free(message);
	free(line);
	puts("actual Go signature accepted by production strict verifier and OpenSSL");
}

int main(int argc, char **argv)
{
	if (argc == 2 && !strcmp(argv[1], "--verify-go")) {
		verify_go();
		return 0;
	}
	assert(argc == 1);
	rfc8032();
	actual_signatures();
	point_policy();
	puts("strict Ed25519 production primitive: PASS");
	return 0;
}
