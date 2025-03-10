const API_URL = "/api";

export interface User {
  id: string;
  username: string;
  joined_at: string;
}

export interface Video {
  id: string;
  title: string;
  description?: string;
  created_at: string;
  creator: User;
  views: number;
  processing: boolean;
}

export type ProgressObject = Record<string, number>;

export interface Progress {
  clips: ProgressObject;
}

// Client only
export const getVideos = async (): Promise<Video[]> => {
  const response = await fetch(`${API_URL}/clips`, {
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
    },
  });
  if (response.status === 204) {
    return [];
  }
  return response.json();
};

export const getVideo = async (videoId: string): Promise<Video | null> => {
  const response = await fetch(`${API_URL}/clips/${videoId}`, {
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
    },
  });
  if (!response.ok) {
    return null;
  }
  return response.json();
};

export const getUsersVideos = async (userId: string): Promise<Video[]> => {
  const response = await fetch(`${API_URL}/users/${userId}/clips`, {
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
    },
  });
  if (response.status === 204) {
    return [];
  }
  return response.json();
};

export const getUser = async (): Promise<User | undefined> => {
  const response = await fetch(`${API_URL}/users/me`, {
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
    },
  });
  if (response.ok) {
    return response.json();
  }
};

export const register = async (username: string, password: string): Promise<Response> => {
  const response = await fetch(`${API_URL}/auth/register`, {
    method: "POST",
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify({ username, password }),
  });
  return response;
};

export const login = async (username: string, password: string): Promise<boolean> => {
  const response = await fetch(`${API_URL}/auth/login`, {
    method: "POST",
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify({ username, password }),
  });
  return response.ok;
};

export const logout = async (): Promise<boolean> => {
  const response = await fetch(`${API_URL}/auth/logout`, {
    method: "POST",
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
    },
  });
  return response.ok;
};
